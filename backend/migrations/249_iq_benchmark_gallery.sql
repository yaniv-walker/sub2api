-- 模型智商检测作品展（Pelican Benchmark 风格）
--
-- 四张新表：题目 / 检测任务 / 执行记录 / 产出。
-- 全部为新增表，不修改任何既有表结构（不给 users 加列、不给 usage_logs 加列、
-- 不扩展 users_signup_source_check、不新增 role 枚举值）。
--
-- 功能默认关闭：本次迁移只建表，调度器在 settings.enabled=false 时不启动，
-- 因此迁移上线后本库不会产生任何行、任何出站调用。

CREATE TABLE IF NOT EXISTS iq_benchmark_cases (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    -- image = 走 /v1/images/generations；text / code = 走 /v1/chat/completions
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('image', 'text', 'code')),
    prompt TEXT NOT NULL,
    system_prompt TEXT,
    params JSONB NOT NULL DEFAULT '{}'::jsonb,
    expect_kind VARCHAR(16) NOT NULL CHECK (expect_kind IN ('image_url', 'svg', 'markdown', 'text')),
    -- 首版不实现 LLM 裁判：llm_judge 保留枚举位但执行侧恒不启用。
    scoring VARCHAR(16) NOT NULL DEFAULT 'manual'
        CHECK (scoring IN ('manual', 'rule', 'manual_rule', 'llm_judge')),
    llm_judge_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- 内置题（鹈鹕骑自行车）不可删除，可禁用。
    builtin BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (length(btrim(name)) > 0),
    CHECK (length(btrim(prompt)) > 0)
);

CREATE INDEX IF NOT EXISTS idx_iq_case_listing
    ON iq_benchmark_cases (enabled, sort_order, id);

CREATE TABLE IF NOT EXISTS iq_benchmark_tasks (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    -- 故意不加 FK：加了会让上游「删除分组」被本表阻断，改变既有行为语义。
    -- 分组被删除后任务在下一轮执行时记失败（error_code=group_missing）并自动禁用。
    group_id BIGINT NOT NULL,
    model VARCHAR(200) NOT NULL,
    -- 题目 ID 有序数组；运行时按顺序解析，已失效的 ID 被跳过。
    case_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- 间隔 15 分钟 ~ 7 天；抖动由 service 层另校验 interval*60 - jitter >= 900。
    interval_minutes INTEGER NOT NULL CHECK (interval_minutes >= 15 AND interval_minutes <= 10080),
    jitter_seconds INTEGER NOT NULL DEFAULT 0 CHECK (jitter_seconds >= 0),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(24) NOT NULL DEFAULT 'idle'
        CHECK (status IN ('idle', 'running', 'quota_exhausted', 'budget_exhausted',
                          'account_disabled', 'paused_by_admin')),
    status_reason TEXT,
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    -- 多实例去重：领取者标识 + 租约到期时间（FOR UPDATE SKIP LOCKED）。
    lease_owner VARCHAR(128),
    lease_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (length(btrim(name)) > 0),
    CHECK (length(btrim(model)) > 0),
    CHECK (jsonb_typeof(case_ids) = 'array')
);

-- 同一「分组 + 模型」最多只有一个启用中的任务（数据库级强制，非仅应用层检查）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_iq_task_group_model_enabled
    ON iq_benchmark_tasks (group_id, model)
    WHERE enabled = TRUE;

-- 调度扫描路径：到期任务 + 租约可用。
CREATE INDEX IF NOT EXISTS idx_iq_task_due
    ON iq_benchmark_tasks (next_run_at)
    WHERE enabled = TRUE;

CREATE INDEX IF NOT EXISTS idx_iq_task_group_model
    ON iq_benchmark_tasks (group_id, model);

CREATE TABLE IF NOT EXISTS iq_benchmark_runs (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    -- group_id / model 为快照值：任务后续被改也能回溯历史。
    group_id BIGINT NOT NULL,
    model VARCHAR(200) NOT NULL,
    trigger VARCHAR(16) NOT NULL CHECK (trigger IN ('schedule', 'manual', 'retry')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    success_count INTEGER NOT NULL DEFAULT 0 CHECK (success_count >= 0),
    failed_count INTEGER NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    timeout_count INTEGER NOT NULL DEFAULT 0 CHECK (timeout_count >= 0),
    blocked_count INTEGER NOT NULL DEFAULT 0 CHECK (blocked_count >= 0),
    total_duration_ms BIGINT NOT NULL DEFAULT 0 CHECK (total_duration_ms >= 0),
    total_cost NUMERIC(20, 10) NOT NULL DEFAULT 0 CHECK (total_cost >= 0),
    total_input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (total_input_tokens >= 0),
    total_output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (total_output_tokens >= 0),
    -- 执行所用内部账号与专用 Key：审计与统计过滤的唯一依据。
    internal_user_id BIGINT NOT NULL,
    internal_api_key_id BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'success', 'partial', 'failed')),
    -- 成本回采标记：usage_logs 异步落库时下一轮补偿回采。
    cost_collected_at TIMESTAMPTZ,
    -- 清理软删。
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_iq_run_task
    ON iq_benchmark_runs (task_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_iq_run_pending_cost
    ON iq_benchmark_runs (created_at)
    WHERE cost_collected_at IS NULL AND deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS iq_benchmark_artifacts (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL,
    task_id BIGINT NOT NULL,
    case_id BIGINT NOT NULL,
    -- 题目快照：题目被改/删后历史产出仍按当时题面回溯。
    case_snapshot JSONB NOT NULL,
    group_id BIGINT NOT NULL,
    model VARCHAR(200) NOT NULL,
    request_params JSONB NOT NULL DEFAULT '{}'::jsonb,
    render_kind VARCHAR(16) NOT NULL
        CHECK (render_kind IN ('image_url', 'svg', 'markdown', 'text')),
    image_url TEXT,
    content TEXT,
    content_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    -- 截断前原文的 SHA256（hex），用于校验与去重。
    content_sha256 CHAR(64),
    -- 命中的危险构造标记（script / event_handler / foreign_object / ...）。
    unsafe_flags JSONB NOT NULL DEFAULT '[]'::jsonb,
    raw_response_ref TEXT,
    duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cost NUMERIC(20, 10) NOT NULL DEFAULT 0 CHECK (cost >= 0),
    status VARCHAR(16) NOT NULL
        CHECK (status IN ('success', 'failed', 'timeout', 'blocked')),
    -- error_code 为内部码，公开侧只下发脱敏后的 error_summary。
    error_code VARCHAR(64),
    error_summary TEXT,
    score NUMERIC(8, 2) CHECK (score IS NULL OR (score >= 0 AND score <= 100)),
    scored_by BIGINT,
    scored_at TIMESTAMPTZ,
    -- 对象存储键：清理时据此删除 blob（provider 未实现 Delete 时只删 DB 行）。
    blob_key TEXT,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (jsonb_typeof(unsafe_flags) = 'array'),
    CHECK (jsonb_typeof(case_snapshot) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_iq_artifact_run
    ON iq_benchmark_artifacts (run_id);

-- 作品展主查询：未删除 + 时间倒序（含 id 保证稳定分页）。
CREATE INDEX IF NOT EXISTS idx_iq_artifact_feed
    ON iq_benchmark_artifacts (created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

-- 筛选组合：分组 / 模型 / 题目 / 时间。
CREATE INDEX IF NOT EXISTS idx_iq_artifact_filters
    ON iq_benchmark_artifacts (group_id, model, case_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_iq_artifact_status
    ON iq_benchmark_artifacts (status, created_at DESC)
    WHERE deleted_at IS NULL;

-- 容量守卫与清理扫描：按 task 保留最新 N 条。
CREATE INDEX IF NOT EXISTS idx_iq_artifact_task_live
    ON iq_benchmark_artifacts (task_id, created_at DESC)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE iq_benchmark_cases IS '模型智商检测题目；builtin=true 的内置题不可删除';
COMMENT ON TABLE iq_benchmark_tasks IS '按「分组 × 模型」定期执行的检测任务；同组合仅一个启用中任务';
COMMENT ON TABLE iq_benchmark_runs IS '任务的每次执行记录（含成本与执行账号快照）';
COMMENT ON TABLE iq_benchmark_artifacts IS '单次执行的产出；题目快照冻结，失败/超时/被拦截同样入库';
COMMENT ON COLUMN iq_benchmark_artifacts.unsafe_flags IS '入库前静态扫描命中的危险构造标记';
COMMENT ON COLUMN iq_benchmark_artifacts.status IS 'success / failed / timeout / blocked（失败作品同样展示）';
COMMENT ON COLUMN iq_benchmark_tasks.lease_owner IS '多实例去重：持有租约的实例标识';
