// Package repository 实现 modeldegradation 的数据访问层。
//
// 设计取舍：全部走原生 SQL，不引入 Ent schema。
// 理由有三条：
//   - 四张表都是「写多读也多」的时序数据，没有复杂关联遍历，Ent 的价值不大；
//   - Ent 需要一个 schema 文件，而本仓库有个坑：schema 文件名若以 _test.go
//     结尾会被当成测试文件。少一个文件就少一处踩坑机会；
//   - 改动 schema 要跑 go generate，而本模块的表结构已经用迁移固定了。
//
// 本包只被 internal/modeldegradation 内部装配，不进入 internal/service 或
// internal/handler，因此不触发 depguard 的 service→repository 约束。
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/modeldegradation"
)

// Repository 是 modeldegradation.Store 的 SQL 实现。
type Repository struct {
	db *sql.DB
}

// New 构造仓储。
func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// 编译期确认实现了接口。
var _ modeldegradation.Store = (*Repository)(nil)

// ── 题库 ──────────────────────────────────────────────────────────────

// ListTasks 读取题库。
func (r *Repository) ListTasks(ctx context.Context, enabledOnly bool) ([]modeldegradation.Task, error) {
	query := `SELECT id, task_type, key, title, prompt_template, expected, sort_order
	          FROM model_degradation_tasks`
	if enabledOnly {
		query += ` WHERE enabled = TRUE`
	}
	query += ` ORDER BY task_type, sort_order, id`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("modeldegradation: 查询题库失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]modeldegradation.Task, 0, 8)
	for rows.Next() {
		var t modeldegradation.Task
		if err := rows.Scan(&t.ID, &t.Type, &t.Key, &t.Title, &t.Prompt, &t.Expected, &t.SortOrder); err != nil {
			return nil, fmt.Errorf("modeldegradation: 扫描题库行失败: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTaskByKey 按题型 + key 取一道题。
func (r *Repository) GetTaskByKey(ctx context.Context, taskType, key string) (*modeldegradation.Task, error) {
	var t modeldegradation.Task
	err := r.db.QueryRowContext(ctx,
		`SELECT id, task_type, key, title, prompt_template, expected, sort_order
		 FROM model_degradation_tasks
		 WHERE task_type = $1 AND key = $2`,
		taskType, key,
	).Scan(&t.ID, &t.Type, &t.Key, &t.Title, &t.Prompt, &t.Expected, &t.SortOrder)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, modeldegradation.ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("modeldegradation: 查询题目失败: %w", err)
	}
	return &t, nil
}

// ── 运行 ──────────────────────────────────────────────────────────────

// 运行记录的列清单。集中在一处，避免 scan 与 select 顺序漂移。
const runColumns = `id, user_id, owner_email_snapshot, base_url_display, model_id,
	protocol, reasoning_effort, render_timeout_seconds, status, verdict,
	logic_passed, logic_total, render_verdict, error_message, error_kind,
	duration_ms, created_at, completed_at, group_id, group_name`

// scanRun 把一行扫进 Run。
func scanRun(scanner interface {
	Scan(dest ...any) error
}) (*modeldegradation.Run, error) {
	var (
		r           modeldegradation.Run
		completedAt sql.NullTime
	)
	err := scanner.Scan(
		&r.ID, &r.UserID, &r.OwnerEmail, &r.BaseURLDisplay, &r.ModelID,
		&r.Protocol, &r.ReasoningEffort, &r.RenderTimeoutSeconds, &r.Status, &r.Verdict,
		&r.LogicPassed, &r.LogicTotal, &r.RenderVerdict, &r.ErrorMessage, &r.ErrorKind,
		&r.DurationMS, &r.CreatedAt, &completedAt, &r.GroupID, &r.GroupName,
	)
	if err != nil {
		return nil, err
	}
	if completedAt.Valid {
		t := completedAt.Time
		r.CompletedAt = &t
	}
	return &r, nil
}

// CreateRun 新建一条运行记录（状态 running）。
func (r *Repository) CreateRun(ctx context.Context, in modeldegradation.RunCreateInput) (*modeldegradation.Run, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO model_degradation_runs
		 (user_id, owner_email_snapshot, base_url_display, model_id, protocol,
		  reasoning_effort, render_timeout_seconds, status, verdict, group_id, group_name)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'running','unknown',$8,$9)
		 RETURNING id`,
		in.UserID, in.OwnerEmail, in.BaseURLDisplay, in.ModelID, in.Protocol,
		in.ReasoningEffort, in.RenderTimeoutSeconds, in.GroupID, in.GroupName,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("modeldegradation: 创建运行记录失败: %w", err)
	}
	return r.GetRun(ctx, id)
}

// InsertItems 批量写入单题占位行，并回填 ID。
//
// 用一条多值 INSERT 而不是逐条：发起时可能有十几道题，
// 逐条往返会让「发起」这个动作明显变慢。
func (r *Repository) InsertItems(ctx context.Context, runID int64, items []modeldegradation.Item) error {
	if len(items) == 0 {
		return nil
	}

	var b strings.Builder
	_, _ = b.WriteString(`INSERT INTO model_degradation_items
		(run_id, task_type, task_key, prompt, expected, verdict, sort_order)
		VALUES `)

	args := make([]any, 0, len(items)*7)
	for i, it := range items {
		if i > 0 {
			_, _ = b.WriteString(",")
		}
		base := i * 7
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7)
		args = append(args,
			runID, it.Type, it.Key, it.Prompt, it.Expected,
			modeldegradation.ItemVerdictUnknown, it.SortOrder)
	}
	_, _ = b.WriteString(" RETURNING id, sort_order")

	rows, err := r.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return fmt.Errorf("modeldegradation: 写入单题占位失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// 回填 ID：按 sort_order 映射回去，保证题目顺序与发起时一致。
	byOrder := make(map[int]int64, len(items))
	for rows.Next() {
		var id int64
		var order int
		if err := rows.Scan(&id, &order); err != nil {
			return fmt.Errorf("modeldegradation: 回填单题 ID 失败: %w", err)
		}
		byOrder[order] = id
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range items {
		if id, ok := byOrder[items[i].SortOrder]; ok {
			items[i].ID = id
		}
	}
	return nil
}

// UpdateItem 更新单题执行结果。
func (r *Repository) UpdateItem(ctx context.Context, itemID int64, in modeldegradation.ItemResultInput) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE model_degradation_items
		 SET response = $2, passed = $3, verdict = $4, detail = $5,
		     artwork = $6, duration_ms = $7
		 WHERE id = $1`,
		itemID, in.Response, in.Passed, in.Verdict, in.Detail, in.Artwork, in.DurationMS,
	)
	if err != nil {
		return fmt.Errorf("modeldegradation: 更新单题结果失败: %w", err)
	}
	return nil
}

// FinishRun 落终态。
func (r *Repository) FinishRun(ctx context.Context, id int64, in modeldegradation.RunFinishInput) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE model_degradation_runs
		 SET status = $2, verdict = $3, logic_passed = $4, logic_total = $5,
		     render_verdict = $6, error_message = $7, error_kind = $8,
		     duration_ms = $9, completed_at = $10
		 WHERE id = $1 AND status = 'running'`,
		id, in.Status, in.Verdict, in.LogicPassed, in.LogicTotal,
		in.RenderVerdict, in.ErrorMessage, in.ErrorKind,
		in.DurationMS, in.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("modeldegradation: 落终态失败: %w", err)
	}
	return nil
}

// CancelRun 把运行标记为 cancelled。
//
// 只在 status='running' 时生效，返回是否命中 —— 这让「两个请求同时点中止」
// 只有一个成功，另一个得到 409 而不是都返回成功。
func (r *Repository) CancelRun(ctx context.Context, id int64, at time.Time) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE model_degradation_runs
		 SET status = 'cancelled', completed_at = $2
		 WHERE id = $1 AND status = 'running'`,
		id, at,
	)
	if err != nil {
		return false, fmt.Errorf("modeldegradation: 中止运行失败: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetRun 读取一条运行记录。
func (r *Repository) GetRun(ctx context.Context, id int64) (*modeldegradation.Run, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM model_degradation_runs WHERE id = $1`, id)
	run, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, modeldegradation.ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("modeldegradation: 读取运行失败: %w", err)
	}
	return run, nil
}

// GetRunDetail 读取运行 + 单题明细。
func (r *Repository) GetRunDetail(ctx context.Context, id int64) (*modeldegradation.RunDetail, error) {
	run, err := r.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, run_id, task_type, task_key, prompt, expected, response,
		        passed, verdict, detail, artwork, duration_ms, sort_order
		 FROM model_degradation_items
		 WHERE run_id = $1
		 ORDER BY sort_order, id`, id)
	if err != nil {
		return nil, fmt.Errorf("modeldegradation: 读取单题明细失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]modeldegradation.Item, 0, 8)
	for rows.Next() {
		var it modeldegradation.Item
		if err := rows.Scan(&it.ID, &it.RunID, &it.Type, &it.Key, &it.Prompt, &it.Expected,
			&it.Response, &it.Passed, &it.Verdict, &it.Detail, &it.Artwork,
			&it.DurationMS, &it.SortOrder); err != nil {
			return nil, fmt.Errorf("modeldegradation: 扫描单题失败: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &modeldegradation.RunDetail{Run: *run, Items: items}, nil
}

// ListRuns 读取检测历史。
//
// 隐藏过滤在 SQL 层做：如果查出来再筛，LIMIT 会先截断再丢弃，
// 前端看到的分页总数和实际能显示的行数会对不上。
func (r *Repository) ListRuns(ctx context.Context, f modeldegradation.RunFilter) ([]modeldegradation.Run, int64, error) {
	where, args := buildRunWhere(f)

	var total int64
	countQuery := `SELECT COUNT(*) FROM model_degradation_runs` + where
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("modeldegradation: 统计运行数失败: %w", err)
	}

	listQuery := `SELECT ` + runColumns + ` FROM model_degradation_runs` + where +
		` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(len(args)+1) +
		` OFFSET $` + strconv.Itoa(len(args)+2)

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	rows, err := r.db.QueryContext(ctx, listQuery, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("modeldegradation: 查询运行列表失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]modeldegradation.Run, 0, limit)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("modeldegradation: 扫描运行失败: %w", err)
		}
		out = append(out, *run)
	}
	return out, total, rows.Err()
}

// buildRunWhere 构造 WHERE 子句与参数。
func buildRunWhere(f modeldegradation.RunFilter) (string, []any) {
	var conds []string
	var args []any

	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	if f.UserID > 0 {
		add("user_id = $%d", f.UserID)
	}
	if f.Verdict != "" {
		add("verdict = $%d", f.Verdict)
	}
	if f.TaskType != "" {
		// 题型不是 runs 的列，用 EXISTS 子查询。
		add(`EXISTS (SELECT 1 FROM model_degradation_items i
		            WHERE i.run_id = model_degradation_runs.id AND i.task_type = $%d)`, f.TaskType)
	}

	if !f.IncludeHidden {
		// 未命中分组的主机记录按 host 隐藏。
		// base_url_display 形如 https://host/***，用前缀匹配即可，
		// 不需要额外存一列 host（线上已有数据也就不用回填）。
		if len(f.ExcludeGroupIDs) > 0 {
			placeholders := make([]string, 0, len(f.ExcludeGroupIDs))
			for _, gid := range f.ExcludeGroupIDs {
				args = append(args, gid)
				placeholders = append(placeholders, "$"+strconv.Itoa(len(args)))
			}
			conds = append(conds, "group_id NOT IN ("+strings.Join(placeholders, ",")+")")
		}
		if len(f.ExcludeHosts) > 0 {
			for _, host := range f.ExcludeHosts {
				args = append(args, "https://"+host+"/%")
				conds = append(conds, "base_url_display NOT LIKE $"+strconv.Itoa(len(args)))
			}
		}
	}

	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// GetStats 读取顶部统计。
func (r *Repository) GetStats(ctx context.Context, f modeldegradation.RunFilter) (modeldegradation.Stats, error) {
	where, args := buildRunWhere(f)

	var (
		stats    modeldegradation.Stats
		normal   int64
		complete int64
	)
	query := `SELECT
	          COUNT(*),
	          COUNT(*) FILTER (WHERE verdict = 'normal' AND status = 'completed'),
	          COUNT(*) FILTER (WHERE status = 'completed')
	          FROM model_degradation_runs` + where
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&stats.RunCount, &normal, &complete); err != nil {
		return stats, fmt.Errorf("modeldegradation: 统计失败: %w", err)
	}
	stats.PassRate = modeldegradation.PassRate(normal, complete)

	// 最近一次已完成检测的耗时。
	var lastDuration sql.NullInt64
	lastQuery := `SELECT duration_ms FROM model_degradation_runs` + where
	if where == "" {
		lastQuery += ` WHERE status = 'completed'`
	} else {
		lastQuery += ` AND status = 'completed'`
	}
	lastQuery += ` ORDER BY created_at DESC LIMIT 1`
	if err := r.db.QueryRowContext(ctx, lastQuery, args...).Scan(&lastDuration); err != nil &&
		!errors.Is(err, sql.ErrNoRows) {
		return stats, fmt.Errorf("modeldegradation: 统计最近耗时失败: %w", err)
	}
	if lastDuration.Valid {
		stats.LastDurationMS = lastDuration.Int64
	}

	return stats, nil
}

// ── 可见性 ────────────────────────────────────────────────────────────

// GetVisibility 读取隐藏配置。
func (r *Repository) GetVisibility(ctx context.Context) (*modeldegradation.Visibility, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT kind, target FROM model_degradation_visibility WHERE hidden = TRUE`)
	if err != nil {
		return nil, fmt.Errorf("modeldegradation: 查询可见性配置失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := &modeldegradation.Visibility{
		HiddenGroupIDs: []int64{},
		HiddenHosts:    []string{},
		Hosts:          []modeldegradation.HostUsage{},
	}
	for rows.Next() {
		var kind, target string
		if err := rows.Scan(&kind, &target); err != nil {
			return nil, fmt.Errorf("modeldegradation: 扫描可见性配置失败: %w", err)
		}
		switch kind {
		case modeldegradation.VisibilityKindGroup:
			if id, err := strconv.ParseInt(target, 10, 64); err == nil {
				out.HiddenGroupIDs = append(out.HiddenGroupIDs, id)
			}
		case modeldegradation.VisibilityKindHost:
			out.HiddenHosts = append(out.HiddenHosts, target)
		}
	}
	return out, rows.Err()
}

// SetGroupHidden 设置分组可见性。
func (r *Repository) SetGroupHidden(ctx context.Context, groupID int64, hidden bool) error {
	return r.upsertVisibility(ctx, modeldegradation.VisibilityKindGroup,
		strconv.FormatInt(groupID, 10), hidden)
}

// SetHostHidden 设置主机可见性。
func (r *Repository) SetHostHidden(ctx context.Context, host string, hidden bool) error {
	return r.upsertVisibility(ctx, modeldegradation.VisibilityKindHost,
		strings.ToLower(strings.TrimSpace(host)), hidden)
}

// upsertVisibility 写入或更新一条可见性配置。
func (r *Repository) upsertVisibility(ctx context.Context, kind, target string, hidden bool) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO model_degradation_visibility (kind, target, hidden)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (kind, target) DO UPDATE
		 SET hidden = EXCLUDED.hidden, updated_at = NOW()`,
		kind, target, hidden,
	)
	if err != nil {
		return fmt.Errorf("modeldegradation: 保存可见性配置失败: %w", err)
	}
	return nil
}

// ListUnmatchedHosts 返回未命中分组的上游主机及记录数。
//
// base_url_display 形如 https://api.example.com/***，
// 用正则取出 host 部分。只统计 group_id = 0 的记录 ——
// 命中分组的记录由分组维度管理，不该在这里重复出现。
func (r *Repository) ListUnmatchedHosts(ctx context.Context) ([]modeldegradation.HostUsage, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT
		   lower(regexp_replace(base_url_display, '^https?://([^/]+).*$', '\1')) AS host,
		   COUNT(*) AS run_count
		 FROM model_degradation_runs
		 WHERE group_id = 0 AND base_url_display <> ''
		 GROUP BY 1
		 ORDER BY run_count DESC, host ASC`)
	if err != nil {
		return nil, fmt.Errorf("modeldegradation: 查询上游主机失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]modeldegradation.HostUsage, 0, 8)
	for rows.Next() {
		var h modeldegradation.HostUsage
		if err := rows.Scan(&h.Host, &h.RunCount); err != nil {
			return nil, fmt.Errorf("modeldegradation: 扫描主机行失败: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
