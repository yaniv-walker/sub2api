-- 250_iq_benchmark_invalid_target.sql
--
-- 目的：为「模型智商检测作品展」的任务状态新增 invalid_target 枚举值。
--
-- 背景（见 docs/iq-benchmark-target-dropdown.md §七.2）：
--   「新建检测任务」的分组/模型改为受约束的级联下拉后，历史任务与运行期都可能出现
--   「目标已失效」的情形：
--     - 分组被禁用或删除（历史任务仍记得旧 group_id）；
--     - 模型已从该分组的可用集合中下线。
--   这类任务不能静默空转重试，需要一个独立状态让它停下来、并在后台明确可见。
--
-- 为什么新开 250 而不是改 249：
--   249_iq_benchmark_gallery.sql 已被 SHA256 校验和锁定，不可修改。
--
-- 为什么不用 paused_by_admin 复用：
--   paused_by_admin 的语义是「管理员主动暂停」，而 invalid_target 是「系统检测到
--   配置失效」。两者恢复语义也不同（前者靠管理员恢复启用，后者需要先改配置）。
--   混用会污染既有枚举值的含义，也会让后台筛选失真。
--
-- 本次迁移是纯结构变更：
--   - 不新增列；
--   - 不改写任何数据行；
--   - 存量任务的状态基线保持原值，只有在下一次被调度器领取时才会被判定为
--     invalid_target（见 service/guard.go 的 TripInvalidTarget）。
--   因此不需要回填脚本，也不会在升级瞬间产生大面积状态抖动。
--
-- 注意：本文件刻意不写 BEGIN/COMMIT。迁移执行器（internal/repository/
-- migrations_runner.go）已为每个迁移文件开启单独事务并在成功后提交；
-- 文件内再写事务控制语句会提前提交外层事务，破坏「一个迁移一个原子单元」的语义。

-- 249 里该 CHECK 是匿名的，PostgreSQL 自动命名为 <表>_<列>_check。
-- 已用一次性 PostgreSQL 18 实例实测确认该命名规律。
ALTER TABLE iq_benchmark_tasks
    DROP CONSTRAINT IF EXISTS iq_benchmark_tasks_status_check;

ALTER TABLE iq_benchmark_tasks
    ADD CONSTRAINT iq_benchmark_tasks_status_check
    CHECK (status IN (
        'idle',
        'running',
        'quota_exhausted',
        'budget_exhausted',
        'account_disabled',
        'paused_by_admin',
        'invalid_target'
    ));

COMMENT ON COLUMN iq_benchmark_tasks.status IS
    'idle / running / quota_exhausted / budget_exhausted / account_disabled / paused_by_admin / invalid_target（分组或模型已失效，需人工改配置）';
