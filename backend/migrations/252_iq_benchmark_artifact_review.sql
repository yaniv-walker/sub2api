-- 252_iq_benchmark_artifact_review.sql
--
-- 补上产出审核的「谁、何时」两列。
--
-- 背景：251 只加了 moderation_status 这个状态列。公开作品展只返回
-- approved，而自测产出在 user_test_require_review=true 时写成 pending，
-- 于是需要一条管理侧的审核动作把它改成 approved。
--
-- 审核是**公开内容**的放行动作，必须留痕：出问题时第一件事就是问
-- 「这条是谁放的」。因此不把审核人塞进既有的 scored_by/scored_at ——
-- 人工评分与内容审核是两件事，混用会让两个字段的含义随上下文漂移。
--
-- 不加外键：与 triggered_by_user_id 同样的理由，用户被删后审核记录
-- 仍应留存（它是管理行为的审计线索，不是用户数据的从属）。

ALTER TABLE iq_benchmark_artifacts
    ADD COLUMN IF NOT EXISTS reviewed_by BIGINT,
    ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ;

COMMENT ON COLUMN iq_benchmark_artifacts.reviewed_by IS
    '审核人 user id；NULL 表示尚未经过人工审核（系统默认 approved 的产出也为 NULL）';
COMMENT ON COLUMN iq_benchmark_artifacts.reviewed_at IS
    '审核时间；与 reviewed_by 同生命周期';
