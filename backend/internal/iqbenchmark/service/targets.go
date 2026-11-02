package service

// 本文件是「检测任务目标（分组 + 模型）」的唯一解析口径。
//
// ## 为什么需要它
//
// 「新建检测任务」原先让管理员手填 group_id 与模型名。手填的错误只有等到真正执行时
// 才会以 404/400 的形式暴露，且管理员无法知道自己填的模型是不是该分组**当下真能调**的。
// 这里把「哪些分组可选、每个分组哪些模型可选」解析成一份受约束的候选集合，
// 前端用它渲染级联下拉，后端用它做创建/更新时的二次校验。
//
// ## 权威口径（四层，必须与网关 GET /v1/models 完全一致）
//
//	第 1 层  GatewayModels.GetAvailableModels(groupID, platform)
//	         └ 按该分组可调度账号的 model_mapping 汇总
//	第 2 层  modelListingSource(platform, layer1, platformDefaultModels(platform))
//	         └ layer1 为空时回退到平台默认集（OpenAI 例外：非 nil 即用）
//	第 3 层  group.ModelAllowlist.FilterForListing(layer2)
//	         └ 仅当白名单 enabled 为真时过滤
//	第 4 层  OpenAI 的补充/退役模型过滤已在第 1 层内部完成
//
// 少做任何一层都会让下拉与实际可调能力不符：
//   - 漏第 2 层：有账号但没配 model_mapping 的分组会显示空下拉，
//     而网关侧其实会回退到平台默认集、这些模型真能调；
//   - 漏第 3 层：会列出被白名单挡掉的模型，选中后在网关侧报错。
//
// ## 唯一的复制粘贴点
//
// 第 2 层依赖上游 `internal/handler/gateway_handler.go` 的 `defaultModelIDsForPlatform`
// 与 `modelListingSource`，二者都是**私有**函数，无法从本模块调用，只能复刻。
// 复刻集中在本文件的 `platformDefaultModelIDs` 与 `modelListingSource` 两个函数内，
// 每个平台分支都标注了对应的上游行号（基线 ef706bb731），便于上游变更时同步。
// `targets_parity_test.go` 对可导出部分做表驱动断言，防止口径漂移。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	upstreamsvc "github.com/Wei-Shaw/sub2api/internal/service"
)

// 候选集合的硬上限（见 docs/iq-benchmark-target-dropdown.md §五）。
//
// 不做分页：分组通常 < 50、单组模型通常 < 100，一次性返回全量最简单也最快；
// 但仍设上限防止异常数据把响应撑爆，超出时置 truncated 而**不静默截断**。
const (
	TargetsMaxGroups         = 200
	TargetsMaxModelsPerGroup = 500
)

// 模型来源标记，供前端区分「运行态真实可用」与「平台默认回退」。
const (
	TargetModelSourceRuntime         = "runtime"
	TargetModelSourcePlatformDefault = "platform_default"
	TargetModelSourceNone            = "none"
)

// 空态原因码。**机器可读码，不在这里做 i18n**：后端不承担文案，
// 前端按码选文案，避免把中文硬编码进 API。
const (
	TargetReasonNoSchedulableAccount    = "NO_SCHEDULABLE_ACCOUNT"
	TargetReasonImageGenerationDisabled = "IMAGE_GENERATION_DISABLED"
	TargetReasonAllowlistFilteredEmpty  = "ALLOWLIST_FILTERED_EMPTY"
	TargetReasonNoModelMapping          = "NO_MODEL_MAPPING"
)

// 能力标记。
const (
	TargetCapabilityText  = "text"
	TargetCapabilityImage = "image"
)

// TargetsGroupStore 是解析目标所需的最小分组仓储能力。
//
// 只声明 ListActive：目标候选**只允许选活跃分组**，禁用/归档分组不该出现在下拉里。
// 用最小接口而非直接依赖上游完整仓储，保持本模块的单向依赖。
type TargetsGroupStore interface {
	ListActive(ctx context.Context) ([]upstreamsvc.Group, error)
}

// GatewayModelLister 是解析模型集合所需的最小网关能力。
//
// 只声明两个导出方法：取值 + 精确失效（供 ?refresh=1 使用）。
// 这样本模块既不需要自己发 HTTP，也不需要构造 Key。
type GatewayModelLister interface {
	GetAvailableModels(ctx context.Context, groupID *int64, platform string) []string
	InvalidateAvailableModelsCache(groupID *int64, platform string)
}

// TargetsService 解析「分组 + 模型」候选集合，并对外提供目标有效性校验。
type TargetsService struct {
	Groups  TargetsGroupStore
	Gateway GatewayModelLister
	// Now 仅供测试注入；不参与缓存（缓存 TTL 由上游 modelsListCache 决定）。
	Now func() time.Time
}

// NewTargetsService 构造目标解析服务。
//
// 两个依赖都允许为 nil：为 nil 时 ListTargets 返回空集合 + 明确原因，
// 而不是 panic —— 管理员后台在依赖缺失时应看到「不可用」而非白屏。
func NewTargetsService(groups TargetsGroupStore, gateway GatewayModelLister) *TargetsService {
	return &TargetsService{Groups: groups, Gateway: gateway, Now: time.Now}
}

// TargetModel 是单个候选模型。
type TargetModel struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
}

// TargetGroup 是单个候选分组及其模型集合。
type TargetGroup struct {
	ID                   int64         `json:"id"`
	Name                 string        `json:"name"`
	Platform             string        `json:"platform"`
	AllowImageGeneration bool          `json:"allow_image_generation"`
	Capabilities         []string      `json:"capabilities"`
	Models               []TargetModel `json:"models"`
	ModelSource          string        `json:"model_source"`
	UnavailableReason    *string       `json:"unavailable_reason"`
}

// Targets 是聚合结果。
type Targets struct {
	Groups    []TargetGroup `json:"groups"`
	Truncated bool          `json:"truncated"`
}

// ListTargets 解析全部活跃分组的候选模型集合。
//
// refresh 为真时先精确失效上游 modelsListCache 再取值（对应 ?refresh=1）。
// 缓存本身完全复用上游实现，本模块不自建缓存层。
func (s *TargetsService) ListTargets(ctx context.Context, refresh bool) (*Targets, error) {
	out := &Targets{Groups: make([]TargetGroup, 0, 8)}
	if s == nil || s.Groups == nil {
		return out, nil
	}

	groups, err := s.Groups.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("iqbenchmark: list active groups: %w", err)
	}

	truncated := false
	if len(groups) > TargetsMaxGroups {
		groups = groups[:TargetsMaxGroups]
		truncated = true
	}

	out.Groups = make([]TargetGroup, 0, len(groups))
	for i := range groups {
		g := groups[i]
		if g.ID <= 0 {
			continue
		}
		groupID := g.ID
		models, source, reason := s.resolveGroupModels(ctx, &groupID, &g, refresh)

		if len(models) > TargetsMaxModelsPerGroup {
			models = models[:TargetsMaxModelsPerGroup]
			truncated = true
		}

		tg := TargetGroup{
			ID:                   g.ID,
			Name:                 g.Name,
			Platform:             g.Platform,
			AllowImageGeneration: groupAllowsImage(&g),
			Capabilities:         groupCapabilities(&g, models),
			Models:               models,
			ModelSource:          source,
		}
		if reason != "" {
			reasonCopy := reason
			tg.UnavailableReason = &reasonCopy
		}
		out.Groups = append(out.Groups, tg)
	}
	out.Truncated = truncated
	return out, nil
}

// ResolveGroupModels 返回单个分组的候选模型集合（供创建/更新校验复用）。
//
// 与 ListTargets 走完全相同的四层口径，避免「下拉能选、保存报错」这种不一致。
func (s *TargetsService) ResolveGroupModels(ctx context.Context, group *upstreamsvc.Group) ([]TargetModel, string) {
	if s == nil || group == nil || group.ID <= 0 {
		return nil, TargetReasonNoSchedulableAccount
	}
	groupID := group.ID
	models, _, reason := s.resolveGroupModels(ctx, &groupID, group, false)
	return models, reason
}

// ActiveGroup 按 ID 解析出一个**活跃**分组。
//
// 只接受 ListActive 返回集合中的分组：禁用/归档分组即便存在也不可作为任务目标，
// 否则任务会在运行期表现为「目标失效」。找不到时返回 ErrTargetGroupUnavailable。
func (s *TargetsService) ActiveGroup(ctx context.Context, groupID int64) (*upstreamsvc.Group, error) {
	if s == nil || s.Groups == nil || groupID <= 0 {
		return nil, iqbenchmark.ErrTargetGroupUnavailable
	}
	groups, err := s.Groups.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("iqbenchmark: list active groups: %w", err)
	}
	for i := range groups {
		if groups[i].ID == groupID {
			return &groups[i], nil
		}
	}
	return nil, iqbenchmark.ErrTargetGroupUnavailable
}

// GroupAllowsImage 是「该分组是否允许生图」的判定，语义与上游
// GroupAllowsImageGeneration 一致（只看 flag，不看平台默认值）。
func GroupAllowsImage(group *upstreamsvc.Group) bool {
	return groupAllowsImage(group)
}

func groupAllowsImage(group *upstreamsvc.Group) bool {
	if group == nil {
		return true
	}
	return group.AllowImageGeneration
}

// groupCapabilities 计算分组级别支持的能力集合。
//
// 由**已解析出的模型列表**决定：没有模型就没有任何能力。
// 这样「能力」与「下拉里真有东西可选」永远一致，不会出现「标了支持生图但一个模型都没有」。
func groupCapabilities(group *upstreamsvc.Group, models []TargetModel) []string {
	caps := make([]string, 0, 2)
	if len(models) == 0 {
		return caps
	}
	caps = append(caps, TargetCapabilityText)
	if groupAllowsImage(group) {
		caps = append(caps, TargetCapabilityImage)
	}
	return caps
}

// resolveGroupModels 是四层口径的唯一实现。
//
// 返回 (模型列表, model_source, 空态原因)。空态原因为空串表示「正常有模型」。
func (s *TargetsService) resolveGroupModels(
	ctx context.Context,
	groupID *int64,
	group *upstreamsvc.Group,
	refresh bool,
) ([]TargetModel, string, string) {
	platform := ""
	if group != nil {
		platform = group.Platform
	}

	// 第 1 层：按可调度账号的 model_mapping 汇总。
	// refresh 时必须先精确失效，否则会读到最多 15s 的旧缓存。
	if refresh && s.Gateway != nil {
		s.Gateway.InvalidateAvailableModelsCache(groupID, platform)
	}
	var available []string
	if s.Gateway != nil {
		available = s.Gateway.GetAvailableModels(ctx, groupID, platform)
	}
	// 注意：nil 与 []string{} 语义不同（见上游 GatewayService.GetAvailableModels）。
	// nil  = 没有账号 / 没有 model_mapping → 可回退平台默认集；
	// []   = 过滤后确实为空（OpenAI 分支）→ 不回退，属于「真的没有」。
	// 第 2 层的 modelListingSource 已按此语义区分，这里不再重复判断。

	// 第 2 层：空集合回退平台默认集（复刻上游 modelListingSource）。
	listing := modelListingSource(platform, available, platformDefaultModelIDs(platform))

	source := TargetModelSourceRuntime
	// 走到回退分支：说明 models 来自平台默认集而非账号映射。
	// OpenAI 分支「非 nil 即用」不会走回退，因此排除。
	if platform != upstreamsvc.PlatformOpenAI && len(available) == 0 && len(listing) > 0 {
		source = TargetModelSourcePlatformDefault
	}

	// 第 3 层：白名单过滤（仅当 enabled）。
	filtered := listing
	if group != nil {
		if before := len(filtered); before > 0 {
			filtered = group.ModelAllowlist.FilterForListing(filtered)
			if len(filtered) == 0 {
				// 第 1 层非空但被白名单过滤成空，是最容易让人困惑的空态。
				return nil, TargetModelSourceNone, TargetReasonAllowlistFilteredEmpty
			}
		}
	}

	if len(filtered) == 0 {
		return nil, TargetModelSourceNone, emptyReasonFor(group)
	}

	models := make([]TargetModel, 0, len(filtered))
	for _, id := range filtered {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		models = append(models, TargetModel{
			ID:           id,
			Capabilities: modelCapabilities(group, id),
		})
	}
	if len(models) == 0 {
		return nil, TargetModelSourceNone, emptyReasonFor(group)
	}
	// 模型列表非空，但分组仍可能「当下跑不了」：平台默认集回退会让一个没有任何
	// 可调度账号的分组也列出模型（这与网关 /v1/models 的行为一致，必须保留）。
	// 这种情况用 unavailable_reason 作为**警告**返回，而不是把模型清空 ——
	// 清空会让管理员无法为「即将上线账号」的分组预先配任务。
	return models, source, groupWarningReason(group)
}

// groupWarningReason 在模型列表非空时，判断分组是否仍存在「跑不了」的风险。
//
// 返回空串表示完全健康。这与 emptyReasonFor 的区别是：这里不清空模型，
// 只是让前端能提示「该分组当前没有可调度账号」。
func groupWarningReason(group *upstreamsvc.Group) string {
	if group == nil {
		return TargetReasonNoSchedulableAccount
	}
	if group.AccountCount == 0 {
		return TargetReasonNoSchedulableAccount
	}
	if group.ActiveAccountCount == 0 {
		return TargetReasonNoSchedulableAccount
	}
	return ""
}

// modelCapabilities 判定单个模型支持的能力。
//
// 判定口径与校验侧完全一致（validateTargetCapability 调用同一函数）：
//   - 分组不允许生图 → 只有 text；
//   - 分组允许生图 → text + image。
//
// 说明：上游没有「按模型名判定是否图像模型」的导出能力（isGeminiImageOutputModel
// 是私有且仅覆盖 Gemini 家族），因此这里不臆造逐模型的图像能力，而是用分组 flag 作为
// 权威门槛 —— 这与运行时门禁 GroupAllowsImageGeneration 的语义完全一致：
// 真正决定能不能出图的是分组开关，而不是模型名字里的 image 字样。
func modelCapabilities(group *upstreamsvc.Group, _ string) []string {
	caps := make([]string, 0, 2)
	caps = append(caps, TargetCapabilityText)
	if groupAllowsImage(group) {
		caps = append(caps, TargetCapabilityImage)
	}
	return caps
}

// emptyReasonFor 区分「模型集合为空」的成因，前端据此显示不同引导文案。
//
// 判定依据全部来自 ListActive 已返回的字段，无需额外查询
// （AccountCount / ActiveAccountCount 由上游 loadAccountCounts 一次聚合查出）。
//
// 注意 AccountCount 是「绑定到该分组的未删除账号数」，与「当前可调度」不等价
// （后者还看限流/临时不可用），因此它只能作为「必然无账号」的充分条件；
// 有账号但模型为空时，再用 ActiveAccountCount 区分「账号都不可调度」与「没配 model_mapping」。
func emptyReasonFor(group *upstreamsvc.Group) string {
	if group == nil {
		return TargetReasonNoSchedulableAccount
	}
	if group.AccountCount == 0 {
		return TargetReasonNoSchedulableAccount
	}
	if group.ActiveAccountCount == 0 {
		return TargetReasonNoSchedulableAccount
	}
	// 有可调度账号却解析不出任何模型：几乎总是因为账号没配 model_mapping，
	// 且平台没有默认集回退（或已开启白名单但白名单为空）。
	return TargetReasonNoModelMapping
}

// ---------- 第 1~2 层的复刻实现（上游私有函数） ----------

// modelListingSource 复刻 upstream internal/handler/gateway_handler.go:1457。
//
// 语义逐条对应（基线 ef706bb731）：
//  1. OpenAI 且 availableModels 非 nil → 直接用（空切片也直接用，不回退）；
//  2. len(availableModels) == 0 → 用平台默认集；
//  3. Anthropic → 合并「可用集 + 默认集」；
//  4. 其余 → 用可用集。
//
// 改动本函数时必须同步上游，否则下拉与 /v1/models 会出现口径差异。
func modelListingSource(platform string, availableModels, fallbackModels []string) []string {
	if platform == upstreamsvc.PlatformOpenAI && availableModels != nil {
		return availableModels
	}
	if len(availableModels) == 0 {
		return fallbackModels
	}
	if platform == upstreamsvc.PlatformAnthropic {
		return mergeModelIDs(availableModels, fallbackModels)
	}
	return availableModels
}

// platformDefaultModelIDs 复刻 upstream internal/handler/gateway_handler.go:1481
// 的 defaultModelIDsForPlatform。
//
// **这是本模块唯一主动复刻上游业务规则的地方**，因此每个分支都保留上游行号注释。
// 它调用的全部是上游**已导出**符号，所以即便有偏差，也是「平台默认模型清单」这一层的
// 数据偏差，不会引入新的业务语义。targets_parity_test.go 对可导出部分做断言。
func platformDefaultModelIDs(platform string) []string {
	switch platform {
	case upstreamsvc.PlatformOpenAI:
		return openai.DefaultModelIDs()
	case upstreamsvc.PlatformGemini:
		ids := make([]string, 0, len(geminicli.DefaultModels))
		for _, model := range geminicli.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	case upstreamsvc.PlatformAntigravity:
		models := antigravity.DefaultModels()
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		return ids
	case upstreamsvc.PlatformAnthropic:
		return claude.DefaultModelIDs()
	case upstreamsvc.PlatformGrok:
		return xai.DefaultModelIDs()
	case upstreamsvc.PlatformOpenCodeGo:
		return upstreamsvc.DefaultOpenCodeGoModelIDs()
	case upstreamsvc.PlatformTypeSafe:
		return []string{"jev-latest"}
	case upstreamsvc.PlatformComposite:
		ids := make([]string, 0)
		seen := make(map[string]struct{})
		for _, concretePlatform := range domain.CompositePrecedencePlatformIDs() {
			// TypeSafe 被有意跳过：jev-latest 只能经 /v1/systemone 使用，
			// 静态回退永不对 LLM 客户端广告它。上游注释同义。
			if concretePlatform == upstreamsvc.PlatformTypeSafe {
				continue
			}
			for _, id := range platformDefaultModelIDs(concretePlatform) {
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
		return ids
	default:
		ids := make([]string, 0, len(claude.DefaultModels))
		for _, model := range claude.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	}
}

// mergeModelIDs 复刻 upstream internal/handler/gateway_handler.go:1534。
// TrimSpace + 丢空 + 去重（保序）。
func mergeModelIDs(primary, secondary []string) []string {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	merged := make([]string, 0, len(primary)+len(secondary))
	for _, models := range [][]string{primary, secondary} {
		for _, model := range models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			merged = append(merged, model)
		}
	}
	return merged
}

// ---------- 校验辅助 ----------

// CheckTaskTarget 实现 scheduler.TargetValidator：校验任务当前的目标是否仍有效。
//
// 只返回目标失效类哨兵（ErrTarget*），让调度器能把「配置失效」（熔断）与
// 「查询失败」（暂时性故障，放行）区分开。
//
// 返回的具体原因会写进 task.status_reason，因此要让人一眼看懂是分组还是模型的问题。
func (s *TargetsService) CheckTaskTarget(ctx context.Context, task *iqbenchmark.Task) error {
	if s == nil || task == nil {
		return nil
	}
	group, err := s.ActiveGroup(ctx, task.GroupID)
	if err != nil {
		if errors.Is(err, iqbenchmark.ErrTargetGroupUnavailable) {
			return fmt.Errorf("%w (group_id=%d)", iqbenchmark.ErrTargetGroupUnavailable, task.GroupID)
		}
		// 非哨兵错误：查询失败，交给调度器按暂时性故障处理。
		return err
	}

	models, reason := s.ResolveGroupModels(ctx, group)
	if len(models) == 0 {
		return fmt.Errorf("%w (group_id=%d, reason=%s)",
			iqbenchmark.ErrTargetModelUnavailable, task.GroupID, reason)
	}
	for i := range models {
		if models[i].ID == task.Model {
			return nil
		}
	}
	return fmt.Errorf("%w (group_id=%d, model=%s)", iqbenchmark.ErrTargetModelUnavailable, task.GroupID, task.Model)
}

// ValidateTarget 校验 (分组, 模型) 是否是当前运行态下的合法目标，
// 并校验该目标能否支撑任务所含的题目类型。
//
// 返回 nil 表示通过。返回的错误是 iqbenchmark 包的哨兵，由 handler 映射成 HTTP 码。
func (s *TargetsService) ValidateTarget(
	ctx context.Context,
	group *upstreamsvc.Group,
	model string,
	caseKinds []string,
) error {
	if group == nil {
		return iqbenchmark.ErrTargetGroupUnavailable
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return iqbenchmark.ErrTargetModelUnavailable
	}

	models, reason := s.ResolveGroupModels(ctx, group)
	if reason != "" || len(models) == 0 {
		// 分组存在但没有任何可调模型：模型必然不属其可用集合。
		return iqbenchmark.ErrTargetModelUnavailable
	}

	found := false
	for i := range models {
		if models[i].ID == model {
			found = true
			break
		}
	}
	if !found {
		return iqbenchmark.ErrTargetModelUnavailable
	}

	// 能力匹配：任务含 image 类题目时必须允许生图。
	// 使用与聚合接口同一套判定（GroupAllowsImage），避免两处口径漂移。
	needsImage := false
	for _, kind := range caseKinds {
		if strings.TrimSpace(kind) == iqbenchmark.CaseKindImage {
			needsImage = true
			break
		}
	}
	if needsImage && !groupAllowsImage(group) {
		return iqbenchmark.ErrTargetCapabilityMismatch
	}
	return nil
}
