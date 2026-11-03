-- 254_iq_benchmark_verdict_index.sql
--
-- 目的：为监控矩阵与通过率统计提供索引支撑。
--
-- 为什么单独一个迁移而不是塞进 253：
--   253 提交后列定义即被 SHA256 校验锁定。索引与列变更分开，
--   将来只调索引时不必触碰已验证的列定义。
--
-- 注意：本文件刻意不写 BEGIN/COMMIT。迁移执行器已按文件包事务。
--
-- 关于「这张索引有没有用」：本条迁移里的两张索引都在 PG 18 上
-- 用真实数据（20 万产出行 / 5 万 run）实测过查询计划，确认被选中。
-- 曾经还建过第三张 idx_iq_artifact_feed_verdict (created_at DESC, id DESC, verdict)，
-- 实测被规划器完全忽略 —— 矩阵查询要取十几列，两种情况都得回表，
-- 而它体积是原 feed 索引的 2.4 倍并拖慢每次产出写入，故删除。
-- 这里保留记录，避免后来者又"顺手加一个覆盖索引"。

-- ---------------------------------------------------------------------------
-- 1) 按判定计数（通过率的分母分子）
-- ---------------------------------------------------------------------------
-- 形状（实测走 Index Only Scan，Heap Fetches: 0）：
--   SELECT verdict, COUNT(*) FROM iq_benchmark_artifacts
--   WHERE deleted_at IS NULL AND created_at >= ? [AND source = ?]
--   GROUP BY verdict
--
-- verdict 是等值分组，所以做前导列；时间紧随其后，
-- 让 created_at 的下界能作为 Index Cond 下推，而不是 Filter。
CREATE INDEX IF NOT EXISTS idx_iq_artifact_verdict_time
    ON iq_benchmark_artifacts (verdict, created_at DESC)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- 2) run 级判定查询
-- ---------------------------------------------------------------------------
-- 形状（实测被选中）：「这个任务最近一次降级是哪次」。
--   SELECT id FROM iq_benchmark_runs
--   WHERE task_id = ? AND verdict = 'degraded' AND deleted_at IS NULL
--   ORDER BY created_at DESC LIMIT 1
--
-- 做成部分索引：降级 run 只占少数（实测约 1/4），
-- 把 verdict='degraded' 提到索引谓词里能显著缩小体积。
CREATE INDEX IF NOT EXISTS idx_iq_run_degraded
    ON iq_benchmark_runs (task_id, created_at DESC)
    WHERE verdict = 'degraded' AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- 3) 刻意不建：矩阵的时间线覆盖索引
-- ---------------------------------------------------------------------------
-- 既有的 idx_iq_artifact_feed (created_at DESC, id DESC) WHERE deleted_at IS NULL
-- 已经完整覆盖矩阵查询（同一 WHERE、同一 ORDER BY、同一 LIMIT 形状）。
-- 矩阵要展示 model / render_kind / status / content 等十几列，
-- 任何覆盖索引都无法避免回表，实测两条计划都是 5 个 buffer。
-- 再建一张只会在每次产出插入时多付一份写放大。
