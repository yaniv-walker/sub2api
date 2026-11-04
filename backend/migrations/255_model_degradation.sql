-- 255_model_degradation.sql
--
-- 「模型降智检测」：用户自带上游凭据，一次性跑一组固定题目，判断该上游
-- 是否被降级。与已有的 iq_benchmark_* 是两套独立能力，故不复用它们的表：
--
--   iq_benchmark_*  定时任务 + 内部账号 + 产出进画廊 + 审核
--   model_degradation_*  用户手动触发 + 用户自带 Key + 结果即时返回
--
-- 硬约束（本迁移的设计前提）：
--   1. 不写 api_key —— 表里根本不存在这一列。Key 只在请求生命周期内存在
--      于内存，既不落库也不进日志。base_url 同理，只存脱敏展示形态。
--   2. 不建外键到 users/groups —— 与 iq_benchmark_tasks 同一条理由：
--      加了会让上游「删除分组」「删除用户」被本表阻断，改变既有行为语义。
--      用户被删后历史仍保留，owner_email_snapshot 保住可读的归属线索。
--   3. 题库（tasks）与运行记录（runs）分开：题库是管理员维护的固定题，
--      运行记录是用户每次发起的快照。改题库不能改动历史记录。
--
-- 迁移上线后本库不会产生任何行、任何出站调用。

-- ── 题库 ────────────────────────────────────────────────────────────────
-- 对应 GET /model-degradation/tasks 的 {logic:[], render:[]} 两组。
CREATE TABLE IF NOT EXISTS model_degradation_tasks (
    id BIGSERIAL PRIMARY KEY,
    -- logic = 有唯一数字答案，靠 expected 判分；
    -- render = 要模型画 SVG，只看能不能渲出结构完整的作品。
    task_type VARCHAR(16) NOT NULL CHECK (task_type IN ('logic', 'render')),
    -- key 是题目的稳定标识，前端用它做选择与去重。
    key VARCHAR(64) NOT NULL,
    title VARCHAR(200) NOT NULL,
    -- render 题的 prompt 含两个占位符，每次发起时替换：
    --   {scene}  随机场景（防止所有 run 画同一张图，便于横向比较构图能力）
    --   {code}   8 位十六进制校验码（防止拿旧图充数）
    -- logic 题不含占位符，原样发送。
    prompt_template TEXT NOT NULL,
    expected TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (length(btrim(key)) > 0),
    CHECK (length(btrim(title)) > 0),
    CHECK (length(btrim(prompt_template)) > 0),
    -- logic 题必须有期望答案，否则永远判不了分。
    -- render 题相反：不靠 expected 判分，强制为空避免误用。
    CHECK (
        (task_type = 'logic' AND length(btrim(expected)) > 0)
        OR (task_type = 'render' AND length(btrim(expected)) = 0)
    )
);

-- 同一题型内 key 唯一；跨题型同名允许（logic 与 render 各有一道主标题时不会撞）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_md_task_type_key
    ON model_degradation_tasks (task_type, key);

CREATE INDEX IF NOT EXISTS idx_md_task_listing
    ON model_degradation_tasks (enabled, task_type, sort_order, id);

-- ── 运行记录 ────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS model_degradation_runs (
    id BIGSERIAL PRIMARY KEY,
    -- 发起者。0 表示发起账号已被删除（历史保留）。
    user_id BIGINT NOT NULL DEFAULT 0,
    -- 归属线索快照：用户被删后仍能显示「谁测过」，且不再回查 users。
    owner_email_snapshot VARCHAR(320) NOT NULL DEFAULT '',

    -- 脱敏后的上游地址，形如 https://api.example.com/***（只保留 scheme+host+path 前缀）。
    -- 原始 base_url 不落库：它可能带内网信息或用户私有查询参数。
    base_url_display VARCHAR(512) NOT NULL DEFAULT '',

    model_id VARCHAR(200) NOT NULL,
    protocol VARCHAR(24) NOT NULL
        CHECK (protocol IN ('chat_completions', 'responses', 'messages')),
    -- 空串 = 未指定。messages 协议不接受该参数，服务层会忽略而不是报错。
    reasoning_effort VARCHAR(16) NOT NULL DEFAULT ''
        CHECK (reasoning_effort IN ('', 'low', 'medium', 'high', 'xhigh', 'max')),
    render_timeout_seconds INTEGER NOT NULL DEFAULT 0
        CHECK (render_timeout_seconds >= 0 AND render_timeout_seconds <= 3600),

    status VARCHAR(16) NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'completed', 'cancelled')),

    -- 四态判定。generated 单独成一态：纯生成模式（只选了 render 题）下
    -- logic_total=0，逻辑题一道没跑，此时谈「正常/降智」没有依据。
    verdict VARCHAR(16) NOT NULL DEFAULT 'unknown'
        CHECK (verdict IN ('normal', 'degraded', 'unknown', 'generated')),

    logic_passed INTEGER NOT NULL DEFAULT 0 CHECK (logic_passed >= 0),
    logic_total INTEGER NOT NULL DEFAULT 0 CHECK (logic_total >= 0),

    -- 生成题判定独立于逻辑题：一个是「答对没有」，一个是「画出来没有」。
    -- 空串 = 未跑生成题。
    render_verdict VARCHAR(16) NOT NULL DEFAULT ''
        CHECK (render_verdict IN ('', 'ok', 'degraded', 'unknown', 'generated')),

    error_message TEXT NOT NULL DEFAULT '',
    -- 12 类归一化错误；空串 = 无错误。与 iqbenchmark 的 ErrorKinds 同名同集合。
    error_kind VARCHAR(32) NOT NULL DEFAULT '',

    duration_ms BIGINT NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,

    -- 用户填的 host 命中了哪个平台分组（用于历史墙的分组维度）。
    -- 0 / 空串 = 未命中任何分组（用户填的是任意外部地址）。
    group_id BIGINT NOT NULL DEFAULT 0,
    group_name VARCHAR(100) NOT NULL DEFAULT '',

    CHECK (length(btrim(model_id)) > 0),
    -- 完成态必须有结束时间；运行中不能有。运行中却写了完成时间的行会让
    -- 「耗时」统计把未完成的算进去。
    CHECK (
        (status = 'running' AND completed_at IS NULL)
        OR (status <> 'running' AND completed_at IS NOT NULL)
    ),
    -- logic_passed 不可能超过 logic_total。
    CHECK (logic_passed <= logic_total)
);

-- 公共墙的主查询路径：按时间倒序，带 limit。
CREATE INDEX IF NOT EXISTS idx_md_run_wall
    ON model_degradation_runs (created_at DESC, id DESC);

-- 「我的检测」与按用户统计。
CREATE INDEX IF NOT EXISTS idx_md_run_user
    ON model_degradation_runs (user_id, created_at DESC);

-- 分组墙按分组聚合。
CREATE INDEX IF NOT EXISTS idx_md_run_group
    ON model_degradation_runs (group_id, created_at DESC);

-- 收尾清理：找出超时未结束的运行（服务重启后遗留的 running 行）。
CREATE INDEX IF NOT EXISTS idx_md_run_stale
    ON model_degradation_runs (status, created_at)
    WHERE status = 'running';

-- ── 单题结果 ────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS model_degradation_items (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES model_degradation_runs(id) ON DELETE CASCADE,
    task_type VARCHAR(16) NOT NULL CHECK (task_type IN ('logic', 'render')),
    task_key VARCHAR(64) NOT NULL DEFAULT '',

    -- 题库题存 key + 快照 prompt；内联自定义题 task_key='custom'，
    -- 只有快照没有题库行。两者共用同一张表，历史才可比。
    prompt TEXT NOT NULL DEFAULT '',
    expected TEXT NOT NULL DEFAULT '',
    response TEXT NOT NULL DEFAULT '',

    passed BOOLEAN NOT NULL DEFAULT FALSE,
    -- 单题判定：ok 答对/画对；degraded 答错/结构退化；generated 画出来了但没判分；
    -- unknown 上游没给可用内容。
    verdict VARCHAR(16) NOT NULL DEFAULT 'unknown'
        CHECK (verdict IN ('ok', 'degraded', 'generated', 'unknown')),

    -- 判定依据的人类可读说明，直接展示给用户。例如：
    --   「命中答案 21」
    --   「未出现期望答案 21，回答中的数字：29, 7, 9, 16」
    --   「上游响应超时，请稍后重试或检查接口服务状态」
    detail TEXT NOT NULL DEFAULT '',

    -- 渲染题的 SVG 源码单独留一份：response 里可能混着解释文字，
    -- 前端要画的是抽出来的 SVG。logic 题为空。
    artwork TEXT NOT NULL DEFAULT '',

    duration_ms BIGINT NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    -- 执行顺序，保证详情页题目顺序与发起时一致。
    sort_order INTEGER NOT NULL DEFAULT 0,

    CHECK (length(btrim(prompt)) > 0)
);

CREATE INDEX IF NOT EXISTS idx_md_item_run
    ON model_degradation_items (run_id, sort_order, id);

-- ── 管理员可见性配置 ────────────────────────────────────────────────────
-- 对应 /admin/model-degradation/history-visibility。
-- 只记录「被隐藏」的项；未出现的项 = 可见。这样新增分组默认可见，
-- 不需要每次新增分组都补一行。
CREATE TABLE IF NOT EXISTS model_degradation_visibility (
    id BIGSERIAL PRIMARY KEY,
    -- group = 平台分组（按 group_id 隐藏整组）；
    -- host  = 用户填的上游主机（未命中分组的那部分记录按 host 隐藏）。
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('group', 'host')),
    -- group 时存 group_id 的十进制字符串；host 时存小写 host。
    target VARCHAR(255) NOT NULL,
    hidden BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (length(btrim(target)) > 0)
);

-- 同一个 target 只有一条配置；切换可见性走 UPDATE 而不是堆行。
CREATE UNIQUE INDEX IF NOT EXISTS uq_md_visibility_kind_target
    ON model_degradation_visibility (kind, target);

COMMENT ON TABLE model_degradation_tasks IS '降智检测题库：logic 靠 expected 判分，render 看结构完整性';
COMMENT ON TABLE model_degradation_runs IS '降智检测的一次执行；用户自带凭据，Key 与本表无关（不落库）';
COMMENT ON TABLE model_degradation_items IS '一次执行里的单题结果（含判定依据 detail 与渲染作品 artwork）';
COMMENT ON TABLE model_degradation_visibility IS '管理员配置的检测历史可见性：只记录被隐藏项，未出现即可见';

COMMENT ON COLUMN model_degradation_runs.base_url_display IS '脱敏后的上游地址，原始值不落库';
COMMENT ON COLUMN model_degradation_runs.render_verdict IS '生成题独立判定，空串表示本次未跑生成题';
COMMENT ON COLUMN model_degradation_runs.verdict IS 'normal/degraded/unknown/generated 四态；纯生成模式为 generated';
COMMENT ON COLUMN model_degradation_items.artwork IS '抽出的 SVG 源码；前端渲染用，logic 题为空';
