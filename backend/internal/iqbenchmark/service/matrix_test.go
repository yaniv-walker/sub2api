//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// 本文件穷举矩阵的合成规则。
//
// 矩阵是「一眼看出模型有没有变笨」的那张图，所以这里的断言重点
// 不是布局好不好看，而是**每个格子到底代表什么**：
// 一格判错，用户就会对着红色格子去查一个其实没问题的模型。

func TestMatrixStateFor(t *testing.T) {
	// 优先级：status 先于 verdict。
	// 调用没跑通时 verdict 说什么都不算数 —— 一条 failed 的产出
	// 若因为 verdict=unknown 被画成「未判定」，用户会以为只是没判分。
	require.Equal(t, MatrixStateError,
		MatrixStateFor(iqbenchmark.ArtifactStatusFailed, iqbenchmark.VerdictUnknown, 1))
	require.Equal(t, MatrixStateError,
		MatrixStateFor(iqbenchmark.ArtifactStatusTimeout, iqbenchmark.VerdictOK, 1),
		"超时即便带了 ok 也必须是 error：调用没成功，判分结果不可信")
	require.Equal(t, MatrixStateError,
		MatrixStateFor(iqbenchmark.ArtifactStatusBlocked, iqbenchmark.VerdictOK, 1))

	require.Equal(t, MatrixStatePassed,
		MatrixStateFor(iqbenchmark.ArtifactStatusSuccess, iqbenchmark.VerdictOK, 1))
	require.Equal(t, MatrixStateIncorrect,
		MatrixStateFor(iqbenchmark.ArtifactStatusSuccess, iqbenchmark.VerdictDegraded, 1))
	require.Equal(t, MatrixStateIncorrect,
		MatrixStateFor(iqbenchmark.ArtifactStatusSuccess, iqbenchmark.VerdictGenerated, 1),
		"没按格式回答、自由发挥也算没通过")
	require.Equal(t, MatrixStateIncorrect,
		MatrixStateFor(iqbenchmark.ArtifactStatusSuccess, iqbenchmark.VerdictUnknown, 1),
		"成功但未判定：不能算通过，否则通过率会被虚高")

	require.Equal(t, MatrixStateEmpty, MatrixStateFor("", "", 0), "没有产出就是空格子")
}

func matrixItems() []iqbenchmark.Artifact {
	return []iqbenchmark.Artifact{
		// 已按 created_at DESC：同一格第一条即最新。
		{ID: 11, CaseID: 1, Model: "m-a", Status: iqbenchmark.ArtifactStatusSuccess,
			Verdict:      iqbenchmark.VerdictOK,
			CaseSnapshot: map[string]any{"name": "题一", "kind": "text"}},
		{ID: 12, CaseID: 1, Model: "m-b", Status: iqbenchmark.ArtifactStatusSuccess,
			Verdict:      iqbenchmark.VerdictDegraded,
			CaseSnapshot: map[string]any{"name": "题一", "kind": "text"}},
		// 同一格的历史记录：必须被丢弃，只保留上面的 11。
		{ID: 99, CaseID: 1, Model: "m-a", Status: iqbenchmark.ArtifactStatusFailed,
			Verdict: iqbenchmark.VerdictUnknown},
		{ID: 21, CaseID: 2, Model: "m-a", Status: iqbenchmark.ArtifactStatusFailed,
			ErrorKind:    iqbenchmark.ErrorKindTimeout,
			CaseSnapshot: map[string]any{"name": "题二", "kind": "image"}},
	}
}

func TestBuildMatrixKeepsNewestPerCell(t *testing.T) {
	m := BuildMatrix(matrixItems(), MatrixMaxCells)

	require.Equal(t, 2, len(m.Cases), "两轴题目各一")
	require.Equal(t, []string{"m-a", "m-b"}, m.Models, "模型轴按首次出现顺序")
	require.Equal(t, 3, m.Total, "3 个格子：题一×m-a、题一×m-b、题二×m-a")

	// 题一 × m-a 必须是 id=11（ok），不能是更晚出现的 id=99（failed）。
	var got *MatrixCell
	for i := range m.Cells {
		if m.Cells[i].CaseID == 1 && m.Cells[i].Model == "m-a" {
			got = &m.Cells[i]
			break
		}
	}
	require.NotNil(t, got)
	require.Equal(t, int64(11), got.ArtifactID, "同一格只保留最新一条")
	require.Equal(t, MatrixStatePassed, got.State)
}

func TestBuildMatrixAxisComesFromData(t *testing.T) {
	m := BuildMatrix(matrixItems(), MatrixMaxCells)

	// 轴由数据决定：题目名取自快照，类型也带出来。
	require.Equal(t, int64(1), m.Cases[0].ID)
	require.Equal(t, "题一", m.Cases[0].Name)
	require.Equal(t, "text", m.Cases[0].Kind)
	require.Equal(t, "题二", m.Cases[1].Name)
	require.Equal(t, "image", m.Cases[1].Kind)
}

func TestBuildMatrixFallsBackToCaseIDWhenSnapshotMissing(t *testing.T) {
	// 快照缺失（历史行、或被裁剪）时不能给空名字：
	// 行轴上出现空白行会让人以为数据坏了。
	items := []iqbenchmark.Artifact{{
		ID: 1, CaseID: 42, Model: "m", Status: iqbenchmark.ArtifactStatusSuccess,
		Verdict: iqbenchmark.VerdictOK,
	}}
	m := BuildMatrix(items, MatrixMaxCells)
	require.Equal(t, "#42", m.Cases[0].Name)
}

func TestBuildMatrixCapsAtMaxCells(t *testing.T) {
	// 造 20 题 × 5 模型 = 100 格，上限 60。
	items := make([]iqbenchmark.Artifact, 0, 100)
	id := int64(1)
	for c := int64(1); c <= 20; c++ {
		for _, m := range []string{"a", "b", "c", "d", "e"} {
			items = append(items, iqbenchmark.Artifact{
				ID: id, CaseID: c, Model: m,
				Status: iqbenchmark.ArtifactStatusSuccess, Verdict: iqbenchmark.VerdictOK,
			})
			id++
		}
	}
	m := BuildMatrix(items, MatrixMaxCells)
	require.Len(t, m.Cells, MatrixMaxCells)
	require.Equal(t, MatrixMaxCells, m.Total)
	require.True(t, m.Truncated, "格子被截断时必须显式告知前端")
}

func TestBuildMatrixExactlyAtCapIsNotTruncated(t *testing.T) {
	// 恰好 60 格是完整矩阵，不能误报截断：
	// 误报会让人以为少了数据，去反复调整筛选条件。
	items := make([]iqbenchmark.Artifact, 0, 60)
	id := int64(1)
	for c := int64(1); c <= 12; c++ {
		for _, m := range []string{"a", "b", "c", "d", "e"} {
			items = append(items, iqbenchmark.Artifact{
				ID: id, CaseID: c, Model: m,
				Status: iqbenchmark.ArtifactStatusSuccess, Verdict: iqbenchmark.VerdictOK,
			})
			id++
		}
	}
	m := BuildMatrix(items, MatrixMaxCells)
	require.Len(t, m.Cells, 60)
	require.False(t, m.Truncated, "恰好填满不算截断")
}

func TestBuildMatrixSkipsInvalidRows(t *testing.T) {
	// case_id 为 0 或模型名为空白：这类行无法定位到格子，
	// 硬塞进去会在轴上产生一个 "0" 或空白项。
	items := []iqbenchmark.Artifact{
		{ID: 1, CaseID: 0, Model: "m", Status: iqbenchmark.ArtifactStatusSuccess},
		{ID: 2, CaseID: 5, Model: "   ", Status: iqbenchmark.ArtifactStatusSuccess},
		{ID: 3, CaseID: 6, Model: "ok", Status: iqbenchmark.ArtifactStatusSuccess,
			Verdict: iqbenchmark.VerdictOK},
	}
	m := BuildMatrix(items, MatrixMaxCells)
	require.Equal(t, 1, m.Total)
	require.Equal(t, int64(6), m.Cases[0].ID)
}

func TestBuildMatrixEmptyInput(t *testing.T) {
	m := BuildMatrix(nil, MatrixMaxCells)
	require.Empty(t, m.Cells)
	require.Empty(t, m.Cases)
	require.Zero(t, m.Total)
	require.False(t, m.Truncated)
}

func TestGetMatrixEmptyWhitelistReturnsEmptyWithoutQuery(t *testing.T) {
	// 白名单为空 → 空矩阵，且**不查库**（store 为 nil 会 panic 即证明没查）。
	store := &galleryStore{}
	svc := NewGalleryService(store, nil)
	m, err := svc.GetMatrix(t.Context(), MatrixQuery{})
	require.NoError(t, err)
	require.Empty(t, m.Cells)
	require.Equal(t, 0, store.listCalls, "白名单为空时不得查库")
}

func TestGetMatrixRejectsGroupOutsideWhitelist(t *testing.T) {
	// 请求点了一个不在白名单里的分组：返回空矩阵，绝不回退成「看全部」。
	// 这是最容易写错的地方 —— fail-closed 的方向必须是「更少」。
	store := &galleryStore{}
	svc := NewGalleryService(store, nil).WithSettings(testSettingsWithExposed(7))
	group := int64(9)
	m, err := svc.GetMatrix(t.Context(), MatrixQuery{GroupID: &group})
	require.NoError(t, err)
	require.Empty(t, m.Cells)
	require.Equal(t, 0, store.listCalls, "白名单外的分组不得查库")
}

func TestGetMatrixPassesWhitelistDownstream(t *testing.T) {
	store := &galleryStore{}
	svc := NewGalleryService(store, nil).WithSettings(testSettingsWithExposed(7, 8))
	_, err := svc.GetMatrix(t.Context(), MatrixQuery{})
	require.NoError(t, err)
	require.Equal(t, []int64{7, 8}, store.lastListFilter.GroupIDs,
		"白名单必须下推给仓储层，不能查完再筛（LIMIT 会先截断）")
	require.Equal(t, iqbenchmark.ModerationApproved, store.lastListFilter.ModerationStatus,
		"矩阵同样只能看审核通过的产出")
}
