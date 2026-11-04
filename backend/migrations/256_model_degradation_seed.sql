-- 256_model_degradation_seed.sql
--
-- 降智检测的内置题库。
--
-- 题目来源：参考实现公开接口 GET /model-degradation/tasks 返回的实际题干，
-- 逐字保留（含「不使用任何外部工具回答以下问题：」前缀与 render 题的
-- 输出约束）。不自行编造「官方题」—— 题库的权威性来自它是固定且公开的一组，
-- 而不是来自题量。要扩充由管理员在题库里加。
--
-- render 题的 prompt 里有 {scene} 与 {code} 两个占位符，每次发起时替换：
--   {scene} 随机场景 —— 若所有 run 都画同一个场景，画出来的东西会趋同，
--           横向比较就失去意义；换场景才能看出模型是不是只会画一种构图。
--   {code}  8 位十六进制校验码 —— 防止拿旧图/模板图充数。校验码必须
--           出现在画面的 SVG <text> 里，一眼能看出是不是本次现画的。
--
-- ON CONFLICT DO UPDATE：重复执行安全（迁移是 forward-only，但开发期
-- 可能重放）。只更新题目内容，不动 sort_order 之外的东西。

INSERT INTO model_degradation_tasks (task_type, key, title, prompt_template, expected, sort_order, enabled)
VALUES
    (
        'logic',
        'candy-shape-flavor',
        '糖果摸取：保证不同形状的苹果味和桃子味',
        '不使用任何外部工具回答以下问题：在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）

| 形状 | 苹果味 | 桃子味 | 西瓜味 |
| 圆形 | 7 | 9 | 8 |
| 五角星形 | 7 | 6 | 4 |',
        '21',
        10,
        TRUE
    ),
    (
        'render',
        'pelican-bicycle',
        '画一只骑自行车的鹈鹕',
        '不使用任何外部工具回答以下问题：创建一幅独立的 SVG 鹈鹕骑自行车 2D 循环动画。主角是一只可爱的鹈鹕，有橙色大嘴、踩在踏板上的蹼足，以及微微张开保持平衡的翅膀。自行车轮子持续转动，脚与踏板动作协调，动画流畅，背景明亮，配色鲜活。本次场景：{scene}。请为这个场景独立构图。在画面右下角用可见的 SVG text 元素显示本次校验码：{code}。只返回一个完整的 SVG，可使用内联 CSS 或 SMIL 动画；不要 HTML、JavaScript、外部图片、外部字体或其他外部资源。请直接在本次回复里输出完整的 SVG 源码，不要创建文件、不要调用工具、不要询问确认，也不要只描述计划或解释做法。',
        '',
        10,
        TRUE
    )
ON CONFLICT (task_type, key) DO UPDATE
SET title = EXCLUDED.title,
    prompt_template = EXCLUDED.prompt_template,
    expected = EXCLUDED.expected,
    enabled = EXCLUDED.enabled,
    updated_at = NOW();
