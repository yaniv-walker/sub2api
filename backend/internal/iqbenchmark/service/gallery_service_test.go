//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// galleryStore 只覆写被测方法，其余经嵌入的 nil 接口兜底。
//
// 嵌入 `Store`（而非逐个实现 30+ 方法）是刻意的：一旦某个被测路径
// 意外调用了未覆写的方法，会立刻 panic 而不是静默返回零值——
// 这正好把「编排逻辑偷跑了别的查询」这种缺陷暴露成测试失败。
type galleryStore struct {
	Store

	enabledRefs int
	allRefs     int
	countErr    error
	removeErr   error
	deleteErr   error
	deletedID   int64
	removedFor  int64
	deleteCalls int

	missing            map[int64]bool
	getArtifactErr     error
	getArtifactCalls   int
	lastBucketLimit    int
	lastBucketSource   string
	lastOverviewSource string
	lastListFilter     iqbenchmark.ArtifactListFilter
	// 记录调用顺序，用于断言「顺序不能变」。
	order []string
}

func (g *galleryStore) CountEnabledTasksReferencingCase(_ context.Context, _ int64) (int, error) {
	g.order = append(g.order, "count-enabled")
	return g.enabledRefs, g.countErr
}

func (g *galleryStore) CountTasksReferencingCase(_ context.Context, _ int64) (int, error) {
	g.order = append(g.order, "count-all")
	return g.allRefs, nil
}

func (g *galleryStore) RemoveCaseFromDisabledTasks(_ context.Context, caseID int64) error {
	g.order = append(g.order, "remove-from-disabled")
	g.removedFor = caseID
	return g.removeErr
}

func (g *galleryStore) DeleteCase(_ context.Context, id int64) error {
	g.order = append(g.order, "delete")
	g.deleteCalls++
	g.deletedID = id
	return g.deleteErr
}

func (g *galleryStore) ListArtifacts(_ context.Context, f iqbenchmark.ArtifactListFilter) (*iqbenchmark.ArtifactListResult, error) {
	g.lastListFilter = f
	return &iqbenchmark.ArtifactListResult{Page: f.Page, PageSize: f.PageSize}, nil
}

func (g *galleryStore) GetArtifact(_ context.Context, id int64) (*iqbenchmark.Artifact, error) {
	g.getArtifactCalls++
	if g.getArtifactErr != nil {
		return nil, g.getArtifactErr
	}
	if g.missing[id] {
		return nil, iqbenchmark.ErrArtifactNotFound
	}
	return &iqbenchmark.Artifact{ID: id}, nil
}

func (g *galleryStore) GetOverviewCounts(_ context.Context, source string) (OverviewCounts, error) {
	g.lastOverviewSource = source
	return OverviewCounts{}, nil
}

func (g *galleryStore) GetOverviewBuckets(_ context.Context, limit int, source string) ([]OverviewBucketRow, error) {
	g.lastBucketLimit = limit
	g.lastBucketSource = source
	return nil, nil
}

// TestDeleteCaseRejectsWhenEnabledTaskReferences 覆盖保护 2：
// 被启用任务引用时必须拒绝，且**不能**先去摘引用、更不能删。
func TestDeleteCaseRejectsWhenEnabledTaskReferences(t *testing.T) {
	store := &galleryStore{enabledRefs: 1}
	svc := NewGalleryService(store, nil)

	err := svc.DeleteCase(context.Background(), 42)
	require.ErrorIs(t, err, iqbenchmark.ErrCaseInUse)
	require.Equal(t, 0, store.deleteCalls, "must not delete when an enabled task references the case")
	require.Zero(t, store.removedFor, "must not touch disabled tasks before rejecting")
	require.Equal(t, []string{"count-enabled"}, store.order,
		"the enabled-reference check must run first and short-circuit")
}

// TestDeleteCaseCleansDisabledTaskRefsThenDeletes 覆盖保护 3：
// 只有停用任务引用时，先摘引用再删除（顺序由 order 断言）。
func TestDeleteCaseCleansDisabledTaskRefsThenDeletes(t *testing.T) {
	store := &galleryStore{enabledRefs: 0, allRefs: 2}
	svc := NewGalleryService(store, nil)

	require.NoError(t, svc.DeleteCase(context.Background(), 7))
	require.Equal(t, []string{"count-enabled", "count-all", "remove-from-disabled", "delete"}, store.order,
		"disabled references must be removed before the row is deleted")
	require.EqualValues(t, 7, store.removedFor)
	require.EqualValues(t, 7, store.deletedID)
	require.Equal(t, 1, store.deleteCalls)
}

// TestDeleteCaseSkipsCleanupWhenUnreferenced 覆盖无引用路径：
// 不应产生多余的写操作。
func TestDeleteCaseSkipsCleanupWhenUnreferenced(t *testing.T) {
	store := &galleryStore{enabledRefs: 0, allRefs: 0}
	svc := NewGalleryService(store, nil)

	require.NoError(t, svc.DeleteCase(context.Background(), 8))
	require.Equal(t, []string{"count-enabled", "count-all", "delete"}, store.order)
	require.Zero(t, store.removedFor, "no disabled task references → no cleanup write")
}

// TestDeleteCasePropagatesStoreErrors 保证失败不会被吞掉：
// 计数失败与摘引用失败都必须中止，且**不得**继续删除。
func TestDeleteCasePropagatesStoreErrors(t *testing.T) {
	boom := errors.New("db down")

	counting := &galleryStore{countErr: boom}
	svc := NewGalleryService(counting, nil)
	err := svc.DeleteCase(context.Background(), 9)
	require.Error(t, err)
	require.ErrorIs(t, err, boom)
	require.Equal(t, 0, counting.deleteCalls, "must not delete after a failed reference count")

	removing := &galleryStore{enabledRefs: 0, allRefs: 1, removeErr: boom}
	svc = NewGalleryService(removing, nil)
	err = svc.DeleteCase(context.Background(), 9)
	require.ErrorIs(t, err, boom)
	require.Equal(t, 0, removing.deleteCalls,
		"must not delete when removing the case from disabled tasks failed")
}

// TestDeleteCasePropagatesBuiltinAndNotFound 保证仓储层的
// 「内置题不可删」与「不存在」两种结论能原样上抛，不被 service 改写。
func TestDeleteCasePropagatesBuiltinAndNotFound(t *testing.T) {
	for _, want := range []error{iqbenchmark.ErrCaseBuiltinUndeletable, iqbenchmark.ErrCaseNotFound} {
		store := &galleryStore{deleteErr: want}
		svc := NewGalleryService(store, nil)
		require.ErrorIs(t, svc.DeleteCase(context.Background(), 3), want)
	}
}

// TestListArtifactsRejectsOversizedPage 覆盖公开列表的 page_size 上限：
// 超过 MaxPublicPageSize 必须**报错**而不是静默截断
// （静默截断会让前端分页器的总页数算错）。
func TestListArtifactsRejectsOversizedPage(t *testing.T) {
	svc := NewGalleryService(&galleryStore{}, nil)
	ctx := context.Background()

	_, err := svc.ListArtifacts(ctx, ArtifactListQuery{Page: 1, PageSize: MaxPublicPageSize + 1})
	require.ErrorIs(t, err, iqbenchmark.ErrPageSizeTooLarge,
		"page_size above the cap must be rejected, not truncated")

	// 恰好等于上限必须放行。
	res, err := svc.ListArtifacts(ctx, ArtifactListQuery{Page: 1, PageSize: MaxPublicPageSize})
	require.NoError(t, err)
	require.Equal(t, MaxPublicPageSize, res.PageSize)
}

// TestCompareTruncatesBeyondEightAndKeepsOrder 覆盖对比接口的边界：
//   - 空输入直接返回空（不发查询）；
//   - 超过 8 个**截断到 8**（而不是报错）——列太多无法并排阅读；
//   - 输出顺序严格按传入 ids，不按仓储返回顺序（列序必须稳定）；
//   - 单个作品被清理（ErrArtifactNotFound）只跳过，不让整次对比失败。
func TestCompareTruncatesBeyondEightAndKeepsOrder(t *testing.T) {
	store := &galleryStore{missing: map[int64]bool{4: true}}
	svc := NewGalleryService(store, nil)
	ctx := context.Background()

	// 空输入：返回空且不查库。
	empty, err := svc.Compare(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
	require.Zero(t, store.getArtifactCalls, "empty compare must not hit the store")

	// 超过 8 个：截断到 8，按传入顺序返回前 8 个。
	ids := []int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	got, err := svc.Compare(ctx, ids)
	require.NoError(t, err)
	require.Len(t, got, 8, "more than 8 ids must be truncated to 8")
	for i, a := range got {
		require.EqualValues(t, ids[i], a.ID, "compare must preserve the requested column order")
	}

	// 不存在的作品被跳过，其余保持顺序。
	got, err = svc.Compare(ctx, []int64{3, 4, 5})
	require.NoError(t, err)
	require.Len(t, got, 2, "a purged artifact must be skipped, not fail the whole compare")
	require.EqualValues(t, 3, got[0].ID)
	require.EqualValues(t, 5, got[1].ID)
}

// TestComparePropagatesRealErrors 保证「不存在」以外的错误必须上抛，
// 不能被当成「作品已被清理」吞掉。
func TestComparePropagatesRealErrors(t *testing.T) {
	boom := errors.New("db down")
	svc := NewGalleryService(&galleryStore{getArtifactErr: boom}, nil)

	_, err := svc.Compare(context.Background(), []int64{1, 2})
	require.ErrorIs(t, err, boom)
}

// TestGetOverviewDefaultsBucketLimit 覆盖总览的 bucketLimit 兜底：
// <=0 时应取默认 50，而不是把 0 直接传给仓储（那会查不出任何桶）。
func TestGetOverviewDefaultsBucketLimit(t *testing.T) {
	store := &galleryStore{}
	svc := NewGalleryService(store, nil)

	_, err := svc.GetOverview(context.Background(), 0, iqbenchmark.SourceScheduled)
	require.NoError(t, err)
	require.Equal(t, 50, store.lastBucketLimit, "non-positive bucketLimit must fall back to 50")
}

// TestGetOverviewPassesSourceThrough 覆盖来源口径的透传与归一化：
// 默认 scheduled 必须原样到达仓储；all 必须变成「不过滤」的空字符串；
// 非法值必须降级为 scheduled 而不是放大成全量。
func TestGetOverviewPassesSourceThrough(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "scheduled stays scheduled", in: iqbenchmark.SourceScheduled, want: iqbenchmark.SourceScheduled},
		{name: "user_test stays user_test", in: iqbenchmark.SourceUserTest, want: iqbenchmark.SourceUserTest},
		{name: "all means no filter", in: iqbenchmark.SourceAll, want: ""},
		{name: "unknown degrades to scheduled", in: "bogus", want: iqbenchmark.SourceScheduled},
		{name: "empty degrades to scheduled", in: "", want: iqbenchmark.SourceScheduled},
		{name: "case and spaces are tolerated", in: "  USER_TEST ", want: iqbenchmark.SourceUserTest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &galleryStore{}
			svc := NewGalleryService(store, nil)

			_, err := svc.GetOverview(context.Background(), 10, tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, store.lastOverviewSource,
				"counts query must receive the normalized source")
			require.Equal(t, tc.want, store.lastBucketSource,
				"buckets query must receive the same normalized source")
		})
	}
}

// TestListArtifactsSourceAndModerationFilter 覆盖列表的两道闸：
//   - source 归一化后进入 filter（默认 scheduled，all 变空串）；
//   - ModeratedOnly 时强制只看 approved（公开侧的可见性规则）。
func TestListArtifactsSourceAndModerationFilter(t *testing.T) {
	store := &galleryStore{}
	svc := NewGalleryService(store, nil)

	_, err := svc.ListArtifacts(context.Background(), ArtifactListQuery{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, iqbenchmark.SourceScheduled, store.lastListFilter.Source,
		"omitted source must default to scheduled")
	require.Empty(t, store.lastListFilter.ModerationStatus,
		"admin path must not implicitly filter by moderation")

	_, err = svc.ListArtifacts(context.Background(), ArtifactListQuery{
		Source:        iqbenchmark.SourceAll,
		ModeratedOnly: true,
		Page:          1,
		PageSize:      10,
	})
	require.NoError(t, err)
	require.Empty(t, store.lastListFilter.Source, "source=all must clear the filter")
	require.Equal(t, iqbenchmark.ModerationApproved, store.lastListFilter.ModerationStatus,
		"public path must only expose approved artifacts")
}

// TestListArtifactsAdminModerationQueue 覆盖审核队列入口。
//
// 这是一条真实缺口：require_review 打开后产出写成 pending，公开侧只给
// approved，所以**必须**有管理侧按状态读取的路径，否则待审产出无处可见。
func TestListArtifactsAdminModerationQueue(t *testing.T) {
	store := &galleryStore{}
	svc := NewGalleryService(store, nil)

	_, err := svc.ListArtifacts(context.Background(), ArtifactListQuery{
		ModerationStatus: iqbenchmark.ModerationPending,
		Page:             1,
		PageSize:         10,
	})
	require.NoError(t, err)
	require.Equal(t, iqbenchmark.ModerationPending, store.lastListFilter.ModerationStatus,
		"admin moderation filter must reach the repository")
}

// TestListArtifactsModeratedOnlyWinsOverAdminFilter 锁死两个字段的优先级。
//
// 合并成一个字段会出现「管理侧设了 pending 却被公开口径强制改成 approved」
// 的静默失效；这条断言保证两者同时出现时公开侧的收紧优先。
func TestListArtifactsModeratedOnlyWinsOverAdminFilter(t *testing.T) {
	store := &galleryStore{}
	svc := NewGalleryService(store, nil)

	_, err := svc.ListArtifacts(context.Background(), ArtifactListQuery{
		ModeratedOnly:    true,
		ModerationStatus: iqbenchmark.ModerationPending,
		Page:             1,
		PageSize:         10,
	})
	require.NoError(t, err)
	require.Equal(t, iqbenchmark.ModerationApproved, store.lastListFilter.ModerationStatus,
		"the public-path restriction must win over an admin status filter")
}
