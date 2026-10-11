# 18｜排空证据语义修正

旧 drain_usage_complete 字段只根据 token 数判断，无法证明供应商最终 usage 或计费完整。本批替换为 drain_usage_observed，表示当前结果中观察到非零 token；不把它当作最终账单依据。原生 Anthropic 原先硬编码 true 已移除，当前保守标记 false，表示没有在该边界核验最终 usage。

drain_observed 仅表示处理函数观察到客户端断开，不证明具体排空时长或资源已收敛。后续仍需显式请求/尝试/排空预算以及终止帧中权威 usage 的独立标记。16、17 中关于完整性的描述由本说明修正。
