-- 253_iq_benchmark_verdict.sql
--
-- 目的：给「模型智商检测」补上「判定」这一维。
--
-- 背景：改造前只有 Status（success / failed / timeout / blocked），
-- 它表达的是「调用跑成没跑成」，不表达「模型答得对不对」。
-- 于是「通过率」只能算调用成功率，模型悄悄变笨时曲线是平的 ——
-- 这正是本次对标要修的东西。
--
-- 三个新增维度，互不替代：
--   iq_benchmark_cases.expected      参考答案。空 = 本题不可判分（图片题）。
--   artifacts.verdict                本题判定（ok / degraded / generated / unknown）。
--   artifacts.render_verdict         渲染形态判定，与 verdict 独立。
--   artifacts.error_kind             面向上游错误形态的 12 类归一化。
--   runs.verdict / logic_total / logic_passed   运行级汇总。
--
-- 为什么不改 Status：Status 是调用事实，是既有的、被前端 RunTable 和
-- 一堆测试依赖的东西。判定是另一个维度，加列而不是改语义。
--
-- 为什么不写回填：功能尚未上线，库里没有存量行，无需 UPDATE。
-- （若将来要补，回填必须做到 count(normal) == 旧 count(success)，
--  否则切换瞬间曲线会跳。这一点记在 docs/iq-benchmark-degradation-alignment.md。）
--
-- 注意：本文件刻意不写 BEGIN/COMMIT。迁移执行器
-- （internal/repository/migrations_runner.go）已为每个迁移文件
-- 开启独立事务并在成功后提交；文件内再写事务控制语句会提前提交
-- 外层事务，破坏「一个迁移一个原子单元」的语义。

-- ---------------------------------------------------------------------------
-- 1) 题目：参考答案
-- ---------------------------------------------------------------------------
-- 刻意可空且无默认值：空串表示「本题不可判分」，
-- 与「答案是空字符串」在语义上必须能区分（后者是合法的判分依据）。
-- 用 NULL 而不是 DEFAULT '' 来表达这种区分。
ALTER TABLE iq_benchmark_cases
    ADD COLUMN IF NOT EXISTS expected TEXT;

COMMENT ON COLUMN iq_benchmark_cases.expected IS
    'logic 类题目的参考答案；NULL/空表示本题不可判分（图片题、开放式生成题）';

-- ---------------------------------------------------------------------------
-- 2) 产出：三态判定 + 渲染判定 + 错误归类
-- ---------------------------------------------------------------------------
ALTER TABLE iq_benchmark_artifacts
    ADD COLUMN IF NOT EXISTS verdict VARCHAR(16);

ALTER TABLE iq_benchmark_artifacts
    ADD COLUMN IF NOT EXISTS render_verdict VARCHAR(16);

ALTER TABLE iq_benchmark_artifacts
    ADD COLUMN IF NOT EXISTS error_kind VARCHAR(32);

COMMENT ON COLUMN iq_benchmark_artifacts.verdict IS
    'ok / degraded / generated / unknown；与 status（调用成败）正交';
COMMENT ON COLUMN iq_benchmark_artifacts.render_verdict IS
    '渲染形态判定，与 verdict 独立；空表示尚未判定';
COMMENT ON COLUMN iq_benchmark_artifacts.error_kind IS
    '面向上游错误形态的 12 类归一化；与 error_code（内部原因、驱动熔断）并存';

-- CHECK 约束：允许 NULL（尚未判定），允许空串（历史/未判定），
-- 其余必须落枚举。用 IF EXISTS 先删再加，保证可重入。
ALTER TABLE iq_benchmark_artifacts
    DROP CONSTRAINT IF EXISTS iq_benchmark_artifacts_verdict_check;
ALTER TABLE iq_benchmark_artifacts
    ADD CONSTRAINT iq_benchmark_artifacts_verdict_check
    CHECK (verdict IS NULL OR verdict IN ('ok', 'degraded', 'generated', 'unknown'));

ALTER TABLE iq_benchmark_artifacts
    DROP CONSTRAINT IF EXISTS iq_benchmark_artifacts_render_verdict_check;
ALTER TABLE iq_benchmark_artifacts
    ADD CONSTRAINT iq_benchmark_artifacts_render_verdict_check
    CHECK (render_verdict IS NULL OR render_verdict IN ('ok', 'degraded', 'generated', 'unknown'));

-- error_kind 的 CHECK 与 Go 侧 ErrorKinds 清单必须逐字一致。
-- 单测 TestMigration253ErrorKindsMatchGoConstants 会比对这两份清单。
ALTER TABLE iq_benchmark_artifacts
    DROP CONSTRAINT IF EXISTS iq_benchmark_artifacts_error_kind_check;
ALTER TABLE iq_benchmark_artifacts
    ADD CONSTRAINT iq_benchmark_artifacts_error_kind_check
    CHECK (error_kind IS NULL OR error_kind IN (
        'auth_failed',
        'forbidden',
        'not_found',
        'timeout',
        'rate_limited',
        'gateway_timeout',
        'server_error',
        'generic',
        'stream_broken',
        'response_incomplete',
        'quota_exceeded',
        'empty_response'
    ));

-- ---------------------------------------------------------------------------
-- 3) 运行：汇总判定与可判计数
-- ---------------------------------------------------------------------------
ALTER TABLE iq_benchmark_runs
    ADD COLUMN IF NOT EXISTS verdict VARCHAR(16);

-- logic_total / logic_passed 是判定口径的分子分母，
-- 与既有的 success_count 等（调用口径）并行，互不替代。
ALTER TABLE iq_benchmark_runs
    ADD COLUMN IF NOT EXISTS logic_total INTEGER NOT NULL DEFAULT 0;

ALTER TABLE iq_benchmark_runs
    ADD COLUMN IF NOT EXISTS logic_passed INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN iq_benchmark_runs.verdict IS
    'normal / degraded / unknown / generated（generated 且 logic_total=0 表示纯生成模式）';
COMMENT ON COLUMN iq_benchmark_runs.logic_total IS
    '本次可判题目数；纯生成模式（只出图）为 0';
COMMENT ON COLUMN iq_benchmark_runs.logic_passed IS
    '本次判分通过的题目数；通过率 = logic_passed / logic_total';

ALTER TABLE iq_benchmark_runs
    DROP CONSTRAINT IF EXISTS iq_benchmark_runs_verdict_check;
ALTER TABLE iq_benchmark_runs
    ADD CONSTRAINT iq_benchmark_runs_verdict_check
    CHECK (verdict IS NULL OR verdict IN ('normal', 'degraded', 'unknown', 'generated'));

-- 计数不能为负、不能超过总数：后者能挡住"分子大于分母"这类
-- 会让通过率超过 100% 的脏数据。
ALTER TABLE iq_benchmark_runs
    DROP CONSTRAINT IF EXISTS iq_benchmark_runs_logic_counts_check;
ALTER TABLE iq_benchmark_runs
    ADD CONSTRAINT iq_benchmark_runs_logic_counts_check
    CHECK (logic_total >= 0 AND logic_passed >= 0 AND logic_passed <= logic_total);

-- ---------------------------------------------------------------------------
-- 4) 题目类型：logic 类自定义题必须有 expected
-- ---------------------------------------------------------------------------
-- 为什么在库层也加一道：服务层会校验，但库层约束能挡住
-- 绕过服务层的写入（例如运维手工 SQL、将来的批量导入）。
-- 只在"非内置 + 非图片类"上要求：
--   - 内置题（鹳雀楼骑车那类）是既有数据，也是管理员随时可禁用的样例，
--     强加约束会让升级在意外存量行上失败；
--   - 图片题天然无 expected（它的产出是图，没有可比的文本）。
--
-- 用 NOT VALID：约束自本迁移起对**新增与更新**的行生效，
-- 但不在此刻回头校验存量行。这样即使将来环境里有预料不到的
-- 存量非内置文本题缺 expected，也不会让升级卡住 ——
-- 那些行下次被编辑时才要求补上。这是一个有意的取舍：
-- 宁可让少数历史行侥幸漏过，也不让升级因为数据问题失败。
ALTER TABLE iq_benchmark_cases
    DROP CONSTRAINT IF EXISTS iq_benchmark_cases_custom_logic_expected_check;
ALTER TABLE iq_benchmark_cases
    ADD CONSTRAINT iq_benchmark_cases_custom_logic_expected_check
    CHECK (
        builtin = TRUE
        OR kind = 'image'
        OR (expected IS NOT NULL AND length(btrim(expected)) > 0)
    ) NOT VALID;
