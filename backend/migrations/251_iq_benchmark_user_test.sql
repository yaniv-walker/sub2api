-- 251_iq_benchmark_user_test.sql
--
-- 目的：支持「用户用自己本站 API Key 一键自测」，产出进入同一个作品展并标「用户自测」。
--
-- 依据：docs/iq-benchmark-user-test.md §五（数据模型）。
--
-- 核心问题：iq_benchmark_artifacts 的 run_id / task_id 都是 NOT NULL，
-- 而用户自测**没有 task、没有 run**（它不是定时任务的一次执行，是用户的单次请求）。
-- 方案对比见 §5.2，采用「方案 A」：
--   - 放宽 run_id / task_id 为可空（用 NULL 如实表达「没有 run」）；
--   - 新增 source 列区分两条链路；
--   - 自测的业务状态（配额/进度/审核锚点）放独立表 iq_benchmark_user_tests。
--
-- 为什么不用「伪造一个虚拟 task/run」（方案 B）：
--   假 run 会进 runs 表并污染执行统计，清理逻辑也会误删它；
--   而且这是对语义的撒谎 —— 用户自测确实不是一次定时执行。
--
-- 为什么不做「单独一张产出表 + UNION」（方案 C）：
--   作品展的列表/详情/筛选/统计/对比五条读路径都要 UNION，
--   而读路径是本功能最公开、高频、已被充分测试的部分。
--   方案 A 让它零改动。
--
-- 注意：本文件刻意不写 BEGIN/COMMIT。迁移执行器
-- （internal/repository/migrations_runner.go）已为每个迁移文件开启单独事务
-- 并在成功后提交；文件内再写事务控制语句会提前提交外层事务。

-- ---------------------------------------------------------------------------
-- 1) 放宽既有列：自测产出没有 run、也没有 task
-- ---------------------------------------------------------------------------
-- 存量行全部有值（249 建表即为 NOT NULL 且已有数据都来自定时链路），
-- 因此 DROP NOT NULL 不动任何数据，也不会在升级瞬间产生行级抖动。
ALTER TABLE iq_benchmark_artifacts ALTER COLUMN run_id DROP NOT NULL;
ALTER TABLE iq_benchmark_artifacts ALTER COLUMN task_id DROP NOT NULL;

-- ---------------------------------------------------------------------------
-- 2) source：产出必须明确来源（必填 + 回填 + 约束，保证永不为空）
-- ---------------------------------------------------------------------------
-- 分三步、同一条迁移内完成，让「必填」与「存量兼容」同时成立：
--   ① 加空列、不给 DEFAULT；② 回填存量；③ SET NOT NULL + CHECK。
--
-- 为什么刻意**不给** DEFAULT 'scheduled'：
--   若给了 DEFAULT，将来新插入的自测行一旦漏写 source，会被**静默**标成
--   'scheduled'，把用户自测错记成定时检测、污染统计口径。
--   不给 DEFAULT，漏写就会直接报 NOT NULL 违例 —— 错误在写入点爆炸，而不是
--   在几天后的报表里才被发现。
ALTER TABLE iq_benchmark_artifacts
    ADD COLUMN IF NOT EXISTS source VARCHAR(16);

UPDATE iq_benchmark_artifacts SET source = 'scheduled' WHERE source IS NULL;

ALTER TABLE iq_benchmark_artifacts ALTER COLUMN source SET NOT NULL;

-- 注意：'all' 只是查询参数取值（表示不过滤），绝不是合法的存储值，
-- 因此这里只允许两个真实来源。
-- DROP + ADD 的写法让本文件可安全重复执行（与 250 一致）。
ALTER TABLE iq_benchmark_artifacts
    DROP CONSTRAINT IF EXISTS iq_artifact_source_check;
ALTER TABLE iq_benchmark_artifacts
    ADD CONSTRAINT iq_artifact_source_check
    CHECK (source IN ('scheduled', 'user_test'));

-- ---------------------------------------------------------------------------
-- 3) 归属用户：自测产出必须知道是谁测的；定时检测为 NULL
-- ---------------------------------------------------------------------------
-- 刻意不加 FK：与 249 里的 group_id 一致，加了会让上游「删除用户」被本表阻断，
-- 改变既有行为语义。
ALTER TABLE iq_benchmark_artifacts
    ADD COLUMN IF NOT EXISTS triggered_by_user_id BIGINT;

-- ---------------------------------------------------------------------------
-- 4) 审核状态：自测默认 pending，定时检测默认 approved
-- ---------------------------------------------------------------------------
-- moderation_status 反过来**给** DEFAULT 'approved'：既有定时检测产出来自
-- 管理员配置的内部账号，是可信任的，无需逐条审核。
-- 自测产出由应用层显式写 'pending'（settings.user_test_require_review=true 时），
-- 或在关闭审核时直接写 'approved'。
ALTER TABLE iq_benchmark_artifacts
    ADD COLUMN IF NOT EXISTS moderation_status VARCHAR(16) NOT NULL DEFAULT 'approved';

ALTER TABLE iq_benchmark_artifacts
    DROP CONSTRAINT IF EXISTS iq_artifact_moderation_status_check;
ALTER TABLE iq_benchmark_artifacts
    ADD CONSTRAINT iq_artifact_moderation_status_check
    CHECK (moderation_status IN ('approved', 'pending', 'rejected'));

-- ---------------------------------------------------------------------------
-- 5) 一致性约束：来源与归属必须自洽（防止写错组合）
-- ---------------------------------------------------------------------------
-- 这条约束把「两条链路」的不变量写进数据库，而不是只写在代码注释里：
--   - 定时检测：没有触发用户（NULL）；
--   - 用户自测：必须有触发用户（NOT NULL）。
-- 少了它，一个写错的 INSERT 就能造出「user_test 但不知道是谁」的孤儿产出，
-- 而公开作品展要按 source 过滤、后台要按用户追责，两者都会因此失真。
ALTER TABLE iq_benchmark_artifacts
    DROP CONSTRAINT IF EXISTS iq_artifact_source_consistent;
ALTER TABLE iq_benchmark_artifacts
    ADD CONSTRAINT iq_artifact_source_consistent CHECK (
        (source = 'scheduled' AND triggered_by_user_id IS NULL)
        OR (source = 'user_test' AND triggered_by_user_id IS NOT NULL)
    );

-- ---------------------------------------------------------------------------
-- 6) 题目可选性：哪些题允许用户自测
-- ---------------------------------------------------------------------------
-- 默认 FALSE：新题默认不开自测，必须管理员显式放开。
-- 这是「防止公开页被低质题目污染」的第一道闸。
ALTER TABLE iq_benchmark_cases
    ADD COLUMN IF NOT EXISTS allow_user_test BOOLEAN NOT NULL DEFAULT FALSE;

-- ---------------------------------------------------------------------------
-- 7) 自测业务表：配额、进度、审核锚点
-- ---------------------------------------------------------------------------
-- 与 artifacts 的分工：artifacts 存「产出内容」（作品展的读路径），
-- 本表存「一次自测请求的业务状态」（配额扣减、并发、去重、审核流转）。
-- 两者用 artifact_id 单向关联，artifacts 不反向感知本表 ——
-- 这样即使本表被整表清空，作品展也完全不受影响。
CREATE TABLE IF NOT EXISTS iq_benchmark_user_tests (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    case_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    model VARCHAR(200) NOT NULL,
    -- 产出写入后回填；失败路径可能始终为 NULL。
    artifact_id BIGINT,
    status VARCHAR(16) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'success', 'failed', 'blocked', 'rejected')),
    error_code VARCHAR(64),
    error_summary TEXT,
    estimated_cost NUMERIC(20, 10) NOT NULL DEFAULT 0 CHECK (estimated_cost >= 0),
    actual_cost NUMERIC(20, 10) NOT NULL DEFAULT 0 CHECK (actual_cost >= 0),
    -- user + case + model + (分钟级时间片) 的哈希，用于 10 分钟去重窗口。
    -- 存哈希而不是明文拼接：避免把 user_id/case_id 以可读形式散落到索引里。
    dedupe_key VARCHAR(128) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 用户维度的配额与历史（「我今天的自测」列表）。
CREATE INDEX IF NOT EXISTS idx_iq_usertest_user
    ON iq_benchmark_user_tests (user_id, created_at DESC);

-- 去重窗口查询：WHERE dedupe_key = ? AND created_at > now() - interval '10 min'。
CREATE INDEX IF NOT EXISTS idx_iq_usertest_dedupe
    ON iq_benchmark_user_tests (dedupe_key, created_at DESC);

-- 清理与并发统计只关心未完成的请求。
CREATE INDEX IF NOT EXISTS idx_iq_usertest_pending
    ON iq_benchmark_user_tests (status)
    WHERE status IN ('pending', 'running');

-- 按产出反查自测记录（后台排障：这条作品是谁测的）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_iq_usertest_artifact
    ON iq_benchmark_user_tests (artifact_id)
    WHERE artifact_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 8) 作品展按来源筛选的索引
-- ---------------------------------------------------------------------------
-- 与 idx_iq_artifact_feed 同形，只是多了 source 前导列；
-- 这样「默认查 scheduled」的主路径仍是索引扫描，不回退到全表。
CREATE INDEX IF NOT EXISTS idx_iq_artifact_source_feed
    ON iq_benchmark_artifacts (source, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- 9) 注释
-- ---------------------------------------------------------------------------
COMMENT ON COLUMN iq_benchmark_artifacts.source IS
    'scheduled（定时检测）/ user_test（用户用自己 Key 自测）';
COMMENT ON COLUMN iq_benchmark_artifacts.run_id IS
    '所属执行记录；用户自测没有 run，为 NULL';
COMMENT ON COLUMN iq_benchmark_artifacts.task_id IS
    '所属任务；用户自测没有 task，为 NULL';
COMMENT ON COLUMN iq_benchmark_artifacts.triggered_by_user_id IS
    '触发自测的用户；定时检测为 NULL（由 iq_artifact_source_consistent 约束保证自洽）';
COMMENT ON COLUMN iq_benchmark_artifacts.moderation_status IS
    'approved / pending / rejected；定时检测默认 approved，自测按配置写 pending';
COMMENT ON COLUMN iq_benchmark_cases.allow_user_test IS
    '是否允许用户自测；默认 FALSE，需管理员显式放开';
COMMENT ON TABLE iq_benchmark_user_tests IS
    '用户自测的业务状态（配额/并发/去重/审核锚点）；产出内容仍写 iq_benchmark_artifacts';
