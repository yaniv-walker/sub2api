//go:build unit

package service

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/stretchr/testify/require"
)

// artifactStore 只实现产出服务用到的方法。
type artifactStore struct {
	Store

	created     []ArtifactCreateInput
	countLive   int64
	countErr    error
	purgeByAge  []PurgedArtifact
	purgeByCap  []PurgedArtifact
	purgeCalls  int
	capCalls    int
	runsDeleted int64
	hardDeleted int64
}

func (a *artifactStore) CreateArtifact(_ context.Context, in ArtifactCreateInput) (*iqbenchmark.Artifact, error) {
	a.created = append(a.created, in)
	return &iqbenchmark.Artifact{ID: int64(len(a.created)), Status: in.Status, RenderKind: in.RenderKind}, nil
}

func (a *artifactStore) CountLiveArtifacts(_ context.Context) (int64, error) {
	return a.countLive, a.countErr
}

func (a *artifactStore) PurgeOlderThan(_ context.Context, _ time.Time, _ int) ([]PurgedArtifact, error) {
	a.purgeCalls++
	if a.purgeCalls == 1 {
		return a.purgeByAge, nil
	}
	return nil, nil
}

func (a *artifactStore) PurgeBeyondLimit(_ context.Context, _ int, _ int) ([]PurgedArtifact, error) {
	a.capCalls++
	if a.capCalls == 1 {
		return a.purgeByCap, nil
	}
	return nil, nil
}

func (a *artifactStore) PurgeRunsWithoutArtifacts(_ context.Context) (int64, error) {
	return a.runsDeleted, nil
}

func (a *artifactStore) HardDeleteArtifactsSoftDeletedBefore(_ context.Context, _ time.Time) (int64, error) {
	return a.hardDeleted, nil
}

func (a *artifactStore) HardDeleteRunsSoftDeletedBefore(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (a *artifactStore) GetArtifact(_ context.Context, id int64) (*iqbenchmark.Artifact, error) {
	return &iqbenchmark.Artifact{ID: id}, nil
}

func (a *artifactStore) ScoreArtifact(_ context.Context, _ int64, _ float64, _ int64, _ time.Time) error {
	return nil
}

func (a *artifactStore) SoftDeleteArtifact(_ context.Context, _ int64) (string, error) {
	return "iq-benchmark/k1.png", nil
}

// fakeBlobDeleter 记录删除调用。
type fakeBlobDeleter struct {
	deleted []string
	failOn  string
}

func (f *fakeBlobDeleter) Delete(_ context.Context, key string) error {
	if f.failOn != "" && key == f.failOn {
		return errShouldNotBeQueried{}
	}
	f.deleted = append(f.deleted, key)
	return nil
}

func newTestArtifactService(t *testing.T, store Store, blobs BlobDeleter) *ArtifactService {
	t.Helper()
	return NewArtifactService(store, nil, blobs, nil)
}

func baseArtifactInput() ArtifactCreateInput {
	return ArtifactCreateInput{
		RunID:   1,
		TaskID:  2,
		CaseID:  3,
		GroupID: 4,
		Model:   "gpt-4o",
	}
}

// 正常的 SVG 产出：应判 success、无危险标记、内容保真。
func TestSaveTextArtifactKeepsCleanSVG(t *testing.T) {
	store := &artifactStore{}
	svc := newTestArtifactService(t, store, nil)
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><circle r="5"/></svg>`
	saved, err := svc.SaveTextArtifact(context.Background(), baseArtifactInput(), iqbenchmark.RenderKindSVG, svg)
	require.NoError(t, err)
	require.Equal(t, iqbenchmark.ArtifactStatusSuccess, saved.Status)
	require.Len(t, store.created, 1)
	require.Empty(t, store.created[0].UnsafeFlags)
	require.Contains(t, store.created[0].Content, "<circle")
	require.Len(t, store.created[0].ContentSHA256, 64)
}

// 含 <script> 的产出：整条判 blocked，但正文保留原文供管理员审阅。
func TestSaveTextArtifactBlocksScript(t *testing.T) {
	store := &artifactStore{}
	svc := newTestArtifactService(t, store, nil)
	payload := `<svg><script>alert(1)</script></svg>`
	_, err := svc.SaveTextArtifact(context.Background(), baseArtifactInput(), iqbenchmark.RenderKindSVG, payload)
	require.NoError(t, err)

	in := store.created[0]
	require.Equal(t, iqbenchmark.ArtifactStatusBlocked, in.Status)
	require.Equal(t, iqbenchmark.ErrCodeUnsafeContent, in.ErrorCode)
	require.Contains(t, in.Content, "<script>", "阻断类正文必须保留原文，供管理员审阅")
	require.NotEmpty(t, in.UnsafeFlags)
}

// 事件属性属可剥离类：剥离后仍判 success，作品主体保留。
func TestSaveTextArtifactStripsEventHandlerButKeepsSuccess(t *testing.T) {
	store := &artifactStore{}
	svc := newTestArtifactService(t, store, nil)
	payload := `<svg onload="alert(1)"><rect width="10" height="10"/></svg>`
	_, err := svc.SaveTextArtifact(context.Background(), baseArtifactInput(), iqbenchmark.RenderKindSVG, payload)
	require.NoError(t, err)

	in := store.created[0]
	require.Equal(t, iqbenchmark.ArtifactStatusSuccess, in.Status)
	require.NotContains(t, in.Content, "onload")
	require.Contains(t, in.Content, "<rect")
	require.NotEmpty(t, in.UnsafeFlags)
}

// 超长产出必须被截断并打标，而不是整条拒绝。
func TestSaveTextArtifactTruncatesHugeContent(t *testing.T) {
	store := &artifactStore{}
	svc := newTestArtifactService(t, store, nil)
	huge := strings.Repeat("a", 32<<20) // 32 MiB，超过默认 8 MiB 上限
	_, err := svc.SaveTextArtifact(context.Background(), baseArtifactInput(), iqbenchmark.RenderKindText, huge)
	require.NoError(t, err)

	in := store.created[0]
	require.True(t, in.ContentTruncated)
	require.Equal(t, iqbenchmark.ArtifactStatusSuccess, in.Status)
	require.Contains(t, in.UnsafeFlags, iqbenchmark.ErrCodeTruncated)
}

// 失败作品必须入库，且错误摘要脱敏（不含内部码原文）。
func TestSaveFailedArtifactPersistsSanitizedSummary(t *testing.T) {
	store := &artifactStore{}
	svc := newTestArtifactService(t, store, nil)
	_, err := svc.SaveFailedArtifact(context.Background(), baseArtifactInput(),
		iqbenchmark.ArtifactStatusTimeout, iqbenchmark.ErrCodeTimeout)
	require.NoError(t, err)

	in := store.created[0]
	require.Equal(t, iqbenchmark.ArtifactStatusTimeout, in.Status)
	require.Equal(t, iqbenchmark.ErrCodeTimeout, in.ErrorCode)
	require.NotEmpty(t, in.ErrorSummary)
	require.NotContains(t, in.ErrorSummary, "timeout", "摘要必须是人类可读描述，不能回显内部错误码")
}

func TestSaveFailedArtifactUsesPlaceholderRenderKind(t *testing.T) {
	store := &artifactStore{}
	svc := newTestArtifactService(t, store, nil)
	in := baseArtifactInput()
	in.RenderKind = ""
	_, err := svc.SaveFailedArtifact(context.Background(), in, iqbenchmark.ArtifactStatusFailed, iqbenchmark.ErrCodeUpstreamError)
	require.NoError(t, err)
	require.Equal(t, iqbenchmark.RenderKindText, store.created[0].RenderKind,
		"失败作品需占位形态，否则前端无法渲染占位卡片")
}

func TestEnsureCapacityRejectsAboveOnePointTwoTimes(t *testing.T) {
	store := &artifactStore{countLive: 6001} // max_artifacts 默认 5000 → 上限 6000
	svc := newTestArtifactService(t, store, nil)
	require.ErrorIs(t, svc.EnsureCapacity(context.Background()), iqbenchmark.ErrCapacityExceeded)
}

func TestEnsureCapacityAllowsBelowThreshold(t *testing.T) {
	store := &artifactStore{countLive: 5999}
	svc := newTestArtifactService(t, store, nil)
	require.NoError(t, svc.EnsureCapacity(context.Background()))
}

func TestScoreRejectsOutOfRange(t *testing.T) {
	store := &artifactStore{}
	svc := newTestArtifactService(t, store, nil)
	require.Error(t, svc.Score(context.Background(), 1, -1, 9))
	require.Error(t, svc.Score(context.Background(), 1, 101, 9))
	require.NoError(t, svc.Score(context.Background(), 1, 0, 9))
	require.NoError(t, svc.Score(context.Background(), 1, 100, 9))
}

// 删除时必须尝试删对象存储；失败不回滚数据库删除。
func TestDeleteRemovesBlobWhenSupported(t *testing.T) {
	store := &artifactStore{}
	blobs := &fakeBlobDeleter{}
	svc := newTestArtifactService(t, store, blobs)
	require.NoError(t, svc.Delete(context.Background(), 1))
	require.Equal(t, []string{"iq-benchmark/k1.png"}, blobs.deleted)
}

func TestDeleteToleratesBlobFailure(t *testing.T) {
	store := &artifactStore{}
	blobs := &fakeBlobDeleter{failOn: "iq-benchmark/k1.png"}
	svc := newTestArtifactService(t, store, blobs)
	require.NoError(t, svc.Delete(context.Background(), 1), "blob 删除失败不应让接口报错")
}

// 存储实现不支持删除时：不 panic、只打 WARN。
func TestDeleteWithoutBlobSupport(t *testing.T) {
	store := &artifactStore{}
	svc := newTestArtifactService(t, store, nil)
	require.NoError(t, svc.Delete(context.Background(), 1))
}

func TestCleanupPurgesByAgeThenByCount(t *testing.T) {
	store := &artifactStore{
		purgeByAge:  []PurgedArtifact{{ID: 1, BlobKey: "k1"}, {ID: 2, BlobKey: ""}},
		purgeByCap:  []PurgedArtifact{{ID: 3, BlobKey: "k3"}},
		runsDeleted: 2,
		hardDeleted: 5,
	}
	blobs := &fakeBlobDeleter{}
	svc := newTestArtifactService(t, store, blobs)

	stats, err := svc.Cleanup(context.Background(), time.Now())
	require.NoError(t, err)
	require.Equal(t, 2, stats.ByAge)
	require.Equal(t, 1, stats.ByCount)
	require.Equal(t, int64(2), stats.RunsDeleted)
	require.Equal(t, int64(5), stats.RowsHardDelete)
	// 空 blob_key 不应尝试删除。
	require.Equal(t, []string{"k1", "k3"}, blobs.deleted)
}

func TestCleanupCountsBlobFailures(t *testing.T) {
	store := &artifactStore{purgeByAge: []PurgedArtifact{{ID: 1, BlobKey: "bad"}}}
	blobs := &fakeBlobDeleter{failOn: "bad"}
	svc := newTestArtifactService(t, store, blobs)
	stats, err := svc.Cleanup(context.Background(), time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, stats.BlobsFailed)
}

func TestTruncateWithFlagKeepsUTF8Valid(t *testing.T) {
	// 每个汉字 3 字节；限 4 字节时只能保留 1 个完整字符。
	s := "中文测试"
	out, truncated := truncateWithFlag(s, 4)
	require.True(t, truncated)
	require.Equal(t, "中", out)
	require.True(t, len(out) <= 4)
	require.True(t, utf8.ValidString(out), "截断结果必须是合法 UTF-8")
}

// 遍历所有字节上限，断言任意上限都不会切出非法 UTF-8 序列。
func TestTruncateWithFlagNeverProducesInvalidUTF8(t *testing.T) {
	s := "中a文🙂测试xyz"
	for limit := int64(0); limit <= int64(len(s))+1; limit++ {
		out, _ := truncateWithFlag(s, limit)
		require.True(t, utf8.ValidString(out), "limit=%d 产生了非法 UTF-8: %q", limit, out)
		require.True(t, int64(len(out)) <= limit || limit <= 0, "limit=%d 超出字节上限", limit)
	}
}

func TestTruncateWithFlagNoOpWhenUnderLimit(t *testing.T) {
	out, truncated := truncateWithFlag("abc", 10)
	require.False(t, truncated)
	require.Equal(t, "abc", out)
}

func TestSha256HexIsStable(t *testing.T) {
	require.Equal(t, sha256Hex("abc"), sha256Hex("abc"))
	require.Len(t, sha256Hex("abc"), 64)
	require.NotEqual(t, sha256Hex("abc"), sha256Hex("abd"))
}
