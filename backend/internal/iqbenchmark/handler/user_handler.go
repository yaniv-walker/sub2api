package handler

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/service"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
)

// UserHandler 提供普通用户的**只读**作品展接口。
//
// 三条不可动摇的约束：
//  1. 只读：不存在任何写入方法（评分、删除、触发执行都在管理侧）；
//  2. 白名单 DTO：全部响应经 toPublicArtifact / toPublicCase，绝不下发内部字段；
//  3. 功能关闭即 404：路由层在 enabled=false 时根本不注册这些路径，
//     因此这里不需要额外的 enabled 判断（万一被注册，兜底返回 404）。
type UserHandler struct {
	Gallery  *service.GalleryService
	Settings *iqbenchmark.SettingsProvider
}

// NewUserHandler 构造用户侧 handler。
func NewUserHandler(gallery *service.GalleryService, settings *iqbenchmark.SettingsProvider) *UserHandler {
	return &UserHandler{Gallery: gallery, Settings: settings}
}

// options 依据配置生成公开侧选项。
func (h *UserHandler) options(c *gin.Context) PublicArtifactOptions {
	opts := PublicArtifactOptions{}
	if h.Settings == nil {
		return opts
	}
	settings := h.Settings.Get(c.Request.Context())
	opts.ExposeCost = settings.ExposeCostToUser
	opts.ExposeTokens = settings.ExposeTokensToUser
	opts.ScoringEnabled = settings.ScoringEnabled
	return opts
}

// GetOverview 返回作品展总览。
//
// 公开侧不下发分组名与分组 ID（暴露内部路由拓扑），只给模型名与统计数字。
//
// 统计口径（用户裁决）：默认 scheduled，可通过 ?source= 查询其他口径。
// 默认值刻意**不含**用户自测 —— 否则一次自测刷量就能让「模型排行」失真。
func (h *UserHandler) GetOverview(c *gin.Context) {
	overview, err := h.Gallery.GetOverview(c.Request.Context(), 50, publicSource(c))
	if err != nil {
		writeError(c, err)
		return
	}
	buckets := make([]PublicOverviewBucketDTO, 0, len(overview.Buckets))
	for i := range overview.Buckets {
		b := overview.Buckets[i]
		buckets = append(buckets, PublicOverviewBucketDTO{
			Model:       b.Model,
			Total:       b.Total,
			Success:     b.SuccessCount,
			Failed:      b.FailedCount,
			AvgDuration: b.AvgDurationM,
			LastRunAt:   b.LastRunAt,
		})
	}
	response.Success(c, PublicOverviewDTO{
		TotalArtifacts: overview.TotalArtifacts,
		ImageArtifacts: overview.ImageArtifacts,
		TextArtifacts:  overview.TextArtifacts,
		CaseCount:      overview.CaseCount,
		Buckets:        buckets,
	})
}

// ListArtifacts 分页列出作品。
//
// page_size > 50 直接 400（不静默截断）。
func (h *UserHandler) ListArtifacts(c *gin.Context) {
	query, ok := publicListQuery(c)
	if !ok {
		return
	}
	result, err := h.Gallery.ListArtifacts(c.Request.Context(), query)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Paginated(c, toPublicArtifacts(result.Items, h.options(c)),
		result.Total, result.Page, result.PageSize)
}

// GetArtifact 读取单个作品。
//
// 被阻断（blocked）的作品在 toPublicArtifact 内已剥掉正文，
// 因此这里无需特殊分支 —— 可见性规则只有一处。
func (h *UserHandler) GetArtifact(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	item, err := h.Gallery.GetArtifact(c.Request.Context(), id)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toPublicArtifact(item, h.options(c)))
}

// Compare 返回同题多模型的对比数据（最多 8 个）。
func (h *UserHandler) Compare(c *gin.Context) {
	rawIDs := c.QueryArray("ids")
	if len(rawIDs) == 0 {
		// 也接受逗号分隔写法，前端拼接 query 更方便。
		rawIDs = strings.Split(c.Query("ids"), ",")
	}
	ids := make([]int64, 0, len(rawIDs))
	for _, raw := range rawIDs {
		v := parseOptionalInt64(raw)
		if v != nil {
			ids = append(ids, *v)
		}
	}
	if len(ids) == 0 {
		response.BadRequest(c, "ids is required")
		return
	}
	items, err := h.Gallery.Compare(c.Request.Context(), ids)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"items": toPublicArtifacts(items, h.options(c))})
}

// ListCases 列出可用题目（公开侧不暴露 params / system_prompt / scoring）。
func (h *UserHandler) ListCases(c *gin.Context) {
	items, err := h.Gallery.ListCases(c.Request.Context(), true)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"items": toPublicCases(items)})
}

// publicListQuery 解析公开侧列表查询。
//
// 与 adminListQuery 分开：公开侧的 page_size 上限更严（50），
// 且不接受 group_id（避免枚举内部分组）。
func publicListQuery(c *gin.Context) (service.ArtifactListQuery, bool) {
	page, pageSize := response.ParsePagination(c)
	if pageSize > service.MaxPublicPageSize {
		response.BadRequest(c, "page_size must not exceed 50")
		return service.ArtifactListQuery{}, false
	}
	var since, until *time.Time
	if v := parseOptionalInt64(c.Query("since")); v != nil {
		t := time.Unix(*v, 0)
		since = &t
	}
	if v := parseOptionalInt64(c.Query("until")); v != nil {
		t := time.Unix(*v, 0)
		until = &t
	}
	return service.ArtifactListQuery{
		Model:    c.Query("model"),
		CaseID:   parseOptionalInt64(c.Query("case_id")),
		Status:   c.Query("status"),
		Kind:     c.Query("kind"),
		Source:   publicSource(c),
		Since:    since,
		Until:    until,
		Page:     page,
		PageSize: pageSize,
		// 公开侧只展示审核通过的作品：待审/被拒的产出绝不出现在公开页面。
		ModeratedOnly: true,
	}, true
}

// publicSource 解析公开侧的来源口径参数。
//
// 公开侧默认 scheduled（用户裁决）：一次自测刷量不应拉偏模型排行。
// 传 ?source=all 可查全量，?source=user_test 只看自测。
// 归一化在 service 层统一做（fail-closed 到 scheduled），这里只做去空白。
func publicSource(c *gin.Context) string {
	return strings.TrimSpace(c.Query("source"))
}
