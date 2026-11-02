package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/sanitize"
)

// BlobDeleter 是对象存储删除能力的最小接口。
//
// 刻意不加入 service.ImageStorage：加接口方法会强制所有实现者与测试 fake
// 同步修改（改上游签名）。这里用类型断言获取，未实现时降级为「只删数据库行」。
type BlobDeleter interface {
	Delete(ctx context.Context, key string) error
}

// ArtifactService 负责产出的写入、评分与清理。
type ArtifactService struct {
	Store  Store
	Logger *slog.Logger
	// Settings 提供 max_artifact_bytes / max_artifacts_per_run 等限额。
	Settings *SettingsProvider
	// Blobs 是对象存储删除能力（可为 nil：不支持删除时只软删数据库行）。
	Blobs BlobDeleter
}

// NewArtifactService 构造产出服务。
func NewArtifactService(store Store, settings *SettingsProvider, blobs BlobDeleter, logger *slog.Logger) *ArtifactService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ArtifactService{Store: store, Settings: settings, Blobs: blobs, Logger: logger}
}

// SaveTextArtifact 保存一条文本类产出（SVG / Markdown / 纯文本）。
//
// 处理顺序（顺序不可调换）：
//  1. 抽取候选产物片段（SVG 才抽，纯文本原样）；
//  2. 静态扫描危险构造：阻断类 → status=blocked（保留原文入库供管理员审阅，
//     公开侧永不下发）；可剥离类 → 剥离后入库并记录 unsafe_flags；
//  3. 按 max_artifact_bytes 截断（截断时 status 仍是 success，但置
//     content_truncated=true 并加 truncated 标记，前端提示作品不完整）；
//  4. 计算 sha256 便于去重与审计。
func (s *ArtifactService) SaveTextArtifact(ctx context.Context, in ArtifactCreateInput, renderKind, rawText string) (*iqbenchmark.Artifact, error) {
	settings := s.settings(ctx)
	content := rawText
	if renderKind == iqbenchmark.RenderKindSVG {
		if extracted, ok := sanitize.ExtractSVG(rawText); ok {
			content = extracted
		}
	}

	scan := sanitize.ScanText(content)
	if renderKind == iqbenchmark.RenderKindSVG {
		scan = sanitize.ScanSVG(content)
	} else if renderKind == iqbenchmark.RenderKindMarkdown {
		scan = sanitize.ScanHTML(content)
	}

	in.RenderKind = renderKind
	in.UnsafeFlags = append(in.UnsafeFlags, scan.Flags...)

	if scan.Blocked {
		// 阻断类：整条判 blocked，正文保留原文（管理员可见、公开侧被 DTO 拦掉）。
		in.Status = iqbenchmark.ArtifactStatusBlocked
		in.ErrorCode = iqbenchmark.ErrCodeUnsafeContent
		in.ErrorSummary = sanitize.SafeSummary(iqbenchmark.ErrCodeUnsafeContent)
		in.Content = truncateBytes(content, settings.MaxArtifactBytes)
		if int64(len(in.Content)) < int64(len(content)) {
			in.ContentTruncated = true
		}
		in.ContentSHA256 = sha256Hex(in.Content)
		return s.Store.CreateArtifact(ctx, in)
	}

	cleaned := scan.Content
	if truncated, wasTruncated := truncateWithFlag(cleaned, settings.MaxArtifactBytes); wasTruncated {
		in.ContentTruncated = true
		in.UnsafeFlags = append(in.UnsafeFlags, iqbenchmark.ErrCodeTruncated)
		cleaned = truncated
	}
	in.Content = cleaned
	in.ContentSHA256 = sha256Hex(cleaned)
	if strings.TrimSpace(in.Status) == "" {
		in.Status = iqbenchmark.ArtifactStatusSuccess
	}
	return s.Store.CreateArtifact(ctx, in)
}

// SaveImageArtifact 保存一条图片类产出的引用。
//
// 图片字节的下载与上传由调用方（run 服务）通过既有的 ImageResultUploader
// 完成；本方法只负责记录 URL / blob_key 与状态。
func (s *ArtifactService) SaveImageArtifact(ctx context.Context, in ArtifactCreateInput, imageURL, blobKey string) (*iqbenchmark.Artifact, error) {
	in.RenderKind = iqbenchmark.RenderKindImageURL
	in.ImageURL = imageURL
	in.BlobKey = blobKey
	if strings.TrimSpace(in.Status) == "" {
		in.Status = iqbenchmark.ArtifactStatusSuccess
	}
	return s.Store.CreateArtifact(ctx, in)
}

// SaveFailedArtifact 保存一条失败/超时/被拦截的产出。
//
// 失败作品必须入库：作品展要能展示「这个模型这次失败了」，
// 否则筛选与对比会把失败样本静默隐藏，导致评分分布失真。
//
// 注意：error_summary 必须已脱敏（走 sanitize.SafeSummary），绝不写原始错误。
func (s *ArtifactService) SaveFailedArtifact(ctx context.Context, in ArtifactCreateInput, status, errorCode string) (*iqbenchmark.Artifact, error) {
	in.Status = status
	in.ErrorCode = errorCode
	in.ErrorSummary = sanitize.SafeSummary(errorCode)
	in.RenderKind = firstNonEmptyStr(in.RenderKind, iqbenchmark.RenderKindText)
	return s.Store.CreateArtifact(ctx, in)
}

// EnsureCapacity 在写入前校验总容量。
//
// 超过 max_artifacts 的 1.2 倍即拒绝写入（ErrCapacityExceeded）：
// 留 20% 余量给「清理任务尚未跟上」的正常增长，避免清理一慢就全量拒写。
func (s *ArtifactService) EnsureCapacity(ctx context.Context) error {
	settings := s.settings(ctx)
	if settings.MaxArtifacts <= 0 {
		return nil
	}
	live, err := s.Store.CountLiveArtifacts(ctx)
	if err != nil {
		return fmt.Errorf("iqbenchmark: count live artifacts: %w", err)
	}
	limit := int64(float64(settings.MaxArtifacts) * 1.2)
	if live >= limit {
		s.Logger.Warn("iqbenchmark: artifact capacity exceeded, rejecting new artifacts",
			"live", live, "limit", settings.MaxArtifacts)
		return iqbenchmark.ErrCapacityExceeded
	}
	return nil
}

// Score 写入人工评分。score 必须在 [0, 100]。
func (s *ArtifactService) Score(ctx context.Context, artifactID int64, score float64, scoredBy int64) error {
	if score < 0 || score > 100 {
		return fmt.Errorf("iqbenchmark: score must be within [0, 100], got %v", score)
	}
	if _, err := s.Store.GetArtifact(ctx, artifactID); err != nil {
		return err
	}
	return s.Store.ScoreArtifact(ctx, artifactID, score, scoredBy, time.Now())
}

// Delete 软删除产出，并在对象存储支持时同步删除 blob。
//
// blob 删除失败**不**回滚数据库删除：保留数据库行只会让「已删除」的作品
// 继续出现在列表里，比留下一个孤儿文件更糟。失败以 WARN 记录，由运维按
// 日志清理（见 verification.md 的已知限制）。
func (s *ArtifactService) Delete(ctx context.Context, artifactID int64) error {
	blobKey, err := s.Store.SoftDeleteArtifact(ctx, artifactID)
	if err != nil {
		return err
	}
	s.deleteBlob(ctx, blobKey)
	return nil
}

// deleteBlob 尽力删除对象存储中的文件。
func (s *ArtifactService) deleteBlob(ctx context.Context, blobKey string) {
	if strings.TrimSpace(blobKey) == "" {
		return
	}
	if s.Blobs == nil {
		s.Logger.Warn("iqbenchmark: object storage does not support delete, blob left behind",
			"blob_key", blobKey)
		return
	}
	if err := s.Blobs.Delete(ctx, blobKey); err != nil {
		s.Logger.Warn("iqbenchmark: delete blob failed, blob left behind",
			"blob_key", blobKey, "error", err)
	}
}

// CleanupStats 是一次清理的统计结果。
type CleanupStats struct {
	ByAge          int
	ByCount        int
	RunsDeleted    int64
	RowsHardDelete int64
	BlobsFailed    int
}

// Cleanup 执行一次保留期 + 容量清理。
//
// 三步顺序固定：
//  1. 按 retention_days 软删过期产出；
//  2. 按 max_artifacts 保留最新 N 条、软删其余；
//  3. 软删没有存活产出的 run，再物理删除软删超过保留期的行。
//
// 物理删除放在最后：它只在软删之后才有意义，且必须先跑完前两步，
// 否则会把「本可保留但暂时没有产出」的 run 误删。
func (s *ArtifactService) Cleanup(ctx context.Context, now time.Time) (CleanupStats, error) {
	var stats CleanupStats
	settings := s.settings(ctx)

	// 批次大小避免一次锁住过多行。
	const batch = 200

	if settings.RetentionDays > 0 {
		cutoff := now.AddDate(0, 0, -settings.RetentionDays)
		for {
			purged, err := s.Store.PurgeOlderThan(ctx, cutoff, batch)
			if err != nil {
				return stats, fmt.Errorf("iqbenchmark: purge artifacts older than cutoff: %w", err)
			}
			if len(purged) == 0 {
				break
			}
			stats.ByAge += len(purged)
			stats.BlobsFailed += s.deleteBlobs(ctx, purged)
			if len(purged) < batch {
				break
			}
		}
	}

	if settings.MaxArtifacts > 0 {
		for {
			purged, err := s.Store.PurgeBeyondLimit(ctx, settings.MaxArtifacts, batch)
			if err != nil {
				return stats, fmt.Errorf("iqbenchmark: purge artifacts beyond limit: %w", err)
			}
			if len(purged) == 0 {
				break
			}
			stats.ByCount += len(purged)
			stats.BlobsFailed += s.deleteBlobs(ctx, purged)
			if len(purged) < batch {
				break
			}
		}
	}

	runsDeleted, err := s.Store.PurgeRunsWithoutArtifacts(ctx)
	if err != nil {
		return stats, fmt.Errorf("iqbenchmark: purge empty runs: %w", err)
	}
	stats.RunsDeleted = runsDeleted

	// 物理删除：软删超过 1 天的行，给误删留出可恢复窗口。
	hardCutoff := now.AddDate(0, 0, -1)
	if settings.RetentionDays > 0 && settings.RetentionDays < 1 {
		hardCutoff = now
	}
	artifactsDeleted, err := s.Store.HardDeleteArtifactsSoftDeletedBefore(ctx, hardCutoff)
	if err != nil {
		return stats, fmt.Errorf("iqbenchmark: hard delete artifacts: %w", err)
	}
	runsHardDeleted, err := s.Store.HardDeleteRunsSoftDeletedBefore(ctx, hardCutoff)
	if err != nil {
		return stats, fmt.Errorf("iqbenchmark: hard delete runs: %w", err)
	}
	stats.RowsHardDelete = artifactsDeleted + runsHardDeleted

	if stats.ByAge > 0 || stats.ByCount > 0 || stats.RowsHardDelete > 0 {
		s.Logger.Info("iqbenchmark: cleanup finished",
			"by_age", stats.ByAge, "by_count", stats.ByCount,
			"runs_deleted", stats.RunsDeleted, "rows_hard_deleted", stats.RowsHardDelete,
			"blob_failures", stats.BlobsFailed)
	}
	return stats, nil
}

func (s *ArtifactService) deleteBlobs(ctx context.Context, purged []PurgedArtifact) int {
	failed := 0
	for _, item := range purged {
		if strings.TrimSpace(item.BlobKey) == "" {
			continue
		}
		if s.Blobs == nil {
			failed++
			continue
		}
		if err := s.Blobs.Delete(ctx, item.BlobKey); err != nil {
			failed++
			s.Logger.Warn("iqbenchmark: cleanup delete blob failed",
				"artifact_id", item.ID, "blob_key", item.BlobKey, "error", err)
		}
	}
	return failed
}

func (s *ArtifactService) settings(ctx context.Context) Settings {
	if s.Settings == nil {
		return DefaultSettings()
	}
	return s.Settings.Get(ctx)
}

// truncateBytes 按字节上限截断，且不切断 UTF-8 字符。
func truncateBytes(s string, limit int64) string {
	truncated, _ := truncateWithFlag(s, limit)
	return truncated
}

// truncateWithFlag 截断并返回是否发生了截断。
//
// 按**字节**上限截断（限额配置是字节），但必须保证结果仍是合法 UTF-8：
// 直接切字节会把 3 字节汉字切成 «首字节» + «残片»，落库后前端显示替换字符。
// 因此逐个 rune 累加，遇到放不下就停 —— 这样最坏情况是少保留 1 个字符，
// 但绝不会产生非法序列。
func truncateWithFlag(s string, limit int64) (string, bool) {
	if limit <= 0 || int64(len(s)) <= limit {
		return s, false
	}
	var used int64
	for i, r := range s {
		size := int64(len(string(r)))
		if used+size > limit {
			return s[:i], true
		}
		used += size
	}
	return s, false
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// DecodeSafeJSON 把任意值编码为可入库的 JSONB map（失败时返回空 map）。
func DecodeSafeJSON(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

// ErrArtifactTooLarge 表示单条产出超过 max_artifact_bytes 且策略为拒绝。
//
// 当前策略是「截断而非拒绝」（截断能保留作品的可评判部分），
// 该错误仅用于调用方显式要求严格模式的场景。
var ErrArtifactTooLarge = errors.New("iqbenchmark: artifact exceeds the size limit")
