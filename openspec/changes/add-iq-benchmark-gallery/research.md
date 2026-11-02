# 模型智商检测作品展（Pelican Benchmark 风格）开源调研记录

- **日期**：2026-11-02
- **分支**：`feature/iq-benchmark-gallery`（基于 `main-self`）
- **调研目的**：在自研「分组 × 模型 定期同题检测 → 作品展」功能前，确认开源社区是否已有可复用实现，并给出 License 准入判定。
- **核证方式**：GitHub REST API（`/repos/{owner}/{repo}`）、`raw.githubusercontent.com` 上的 LICENSE 与源码、`simonwillison.net` 页面。凡未取得一手证据的一律标注「未核实」，不做推测性陈述。

---

## 1. 结论摘要

1. **基准出处**：`simonw/pelican-bicycle` 是「pelican riding a bicycle」SVG 基准的原始仓库，提示词为 `Generate an SVG of a pelican riding a bicycle`。该仓库 **无任何 License**（GitHub API 返回 `license: null`，根目录 24 个文件中无 LICENSE），**代码与 20 个 SVG 产物一律不得拷贝**，仅可作为产品思路参考。
2. **默认结论落在「参考设计 + 自研实现」**：所有高同构度的候选（llm-case-benchmark、lmring、arena-hard-auto、OpenCompass、lm-evaluation-harness、FastChat）要么是 Python 服务 / 独立 Node 服务 / Next.js 全栈单体，要么其核心价值在评测算法而非我们的 Go+Vue 落点；直接搬运的侵入成本高于自研。
3. **唯一建议「复用代码」的第三方资产**是两份 MIT/Apache-2.0 的**小型纯函数/库**：
   - `delirious6423/pelican-bench` 的 `_FLAGS` SVG 危险构造正则表（MIT）→ 改写为 Go 正则，用于入库前静态拦截。
   - `joeseesun/llm-case-benchmark` 的 `public/output-classifier.js`（MIT，8.4 KB 纯函数）→ 参考其「完整 SVG / 可恢复 SVG / 覆盖率阈值 0.72」判定逻辑，Go 侧重写（避免引入 UMD JS 到 TS 工程）。
4. **前端零新增依赖**：项目已内置 `dompurify`（`frontend/src/utils/sanitize.ts` 已封装 `sanitizeSvg`），Element Plus **未**引入；因此图片网格与灯箱组件**自研**，不新增 `element-plus` / `vue-easy-lightbox`。
5. **重要安全结论**：LLM 生成的 SVG/HTML 必须当作**不可信输入**处理，且本项目渲染路径是 `image_url`（对象存储直链，`<img src>`，脚本不执行）+ 消毒后 SVG 内联两条并存；对应 XSS 用例必须进单测（详见 `design.md` 的消毒章节）。

---

## 2. 候选仓库清单

License 判定基线：主项目 **LGPL-3.0**。可复用代码 = MIT / Apache-2.0 / BSD / ISC（保留版权与 License 文本）；仅可参考思路 = GPL / AGPL / SSPL / 无 License / 非商用限制。

| # | 仓库 | License（证据） | 最近提交（证据） | 技术栈 | 结论 |
|---|------|----------------|-----------------|--------|------|
| 1 | [simonw/pelican-bicycle](https://github.com/simonw/pelican-bicycle) | **无 License**（API `"license": null`；`/contents/` 无 LICENSE 文件） | 2026-08-01（API `pushed_at`） | Shell + `llm` CLI | 参考（禁拷代码） |
| 2 | [delirious6423/pelican-bench](https://github.com/delirious6423/pelican-bench) | **MIT**（`raw/.../LICENSE`：`MIT License Copyright (c) 2026 pelican-bench contributors`） | 2026-10-25（API `pushed_at`） | Python 3.10 stdlib | **复用（正则表）** |
| 3 | [joeseesun/llm-case-benchmark](https://github.com/joeseesun/llm-case-benchmark) | **MIT**（`raw/.../LICENSE`：`MIT License Copyright (c) 2026 向阳乔木`） | 2026-07-14（API `pushed_at`） | Node22 + Express + SQLite | **复用（分类器逻辑）** |
| 4 | [johnbean393/SVGBench](https://github.com/johnbean393/SVGBench) | **无 License**（API `"license": null`） | 2026-02-12（API `pushed_at`） | Python + ChromeDriver | 放弃（禁拷） |
| 5 | [llm-ring/lmring](https://github.com/llm-ring/lmring) | **Apache-2.0**（API `spdx_id`） | 2026-10-29（API `pushed_at`） | Next.js / React / PostgreSQL | 参考设计 |
| 6 | [lmarena/arena-hard-auto](https://github.com/lmarena/arena-hard-auto) | **Apache-2.0**（API） | 2025-06-21（API） | Python | 放弃（Python 服务） |
| 7 | [open-compass/opencompass](https://github.com/open-compass/opencompass) | **Apache-2.0**（API） | 2026-10-28（API） | Python | 放弃（Python 服务） |
| 8 | [EleutherAI/lm-evaluation-harness](https://github.com/EleutherAI/lm-evaluation-harness) | **MIT**（API） | 2026-09-14（API） | Python | 放弃（Python 框架） |
| 9 | [lm-sys/FastChat](https://github.com/lm-sys/FastChat) | **Apache-2.0**（API） | 2026-05-01（API） | Python + Gradio | 放弃（Python 服务） |
| 10 | [ZJU-REAL/SVGenius](https://github.com/ZJU-REAL/SVGenius) | **Apache-2.0**（API） | 2025-11-10（API） | Python | 放弃（学术评测脚本） |
| 11 | [cure53/DOMPurify](https://github.com/cure53/DOMPurify) | **Apache-2.0**（API `spdx_id`） | 2026-11-01（API） | JS | 已在本项目内 |
| 12 | [element-plus/element-plus](https://github.com/element-plus/element-plus) | **MIT**（API） | 2026-11-02（API） | Vue3 + TS | 不引入（增重） |
| 13 | [antoniandre/splitpanes](https://github.com/antoniandre/splitpanes) | **MIT**（API） | 2026-08-12（API） | Vue3 | 不引入 |
| 14 | [xiongamao/vue-easy-lightbox](https://github.com/xiongamao/vue-easy-lightbox) | **MIT**（API） | 2024-04-03（API） | Vue3 + TS | 不引入（维护停滞） |
| 15 | [bilikaz/compare-llms](https://github.com/bilikaz/compare-llms) | **无 License**（API `"license": null`） | 2026-08-29（API） | React 19 + Vite | 放弃（禁拷） |
| 16 | [Yubo0826/img-comp-slider](https://github.com/Yubo0826/img-comp-slider) | **无 License**（API `"license": null`） | 2024-10-07（API） | Vue3 | 放弃（禁拷） |

> 说明：上表 `stars` 数已核证（pelican-bicycle 218、pelican-bench 4、llm-case-benchmark 9、SVGBench 81、lmring 212、arena-hard-auto 1086、OpenCompass 7561、lm-eval 14363、FastChat 39551、SVGenius 80、DOMPurify 17499、Element Plus 27831、splitpanes 2249、vue-easy-lightbox 466、compare-llms 5、img-comp-slider 2）。为可读性不逐行罗列。

### 2.1 逐项说明

**#1 simonw/pelican-bicycle — 参考，禁拷代码**

- 一手证据：`/contents/` 返回 24 项 = `README.md`、`generate-svgs.sh`、`requirements.txt` + 20 个 `<model>.svg`（如 `claude-3-5-sonnet-20241022.svg` 1328 B、`o1-mini.svg` 2336 B）。无 LICENSE 文件；API `license: null` → 默认保留所有权利。
- README 原文：`Prompt: > Generate an SVG of a pelican riding a bicycle`，并链接 `pelican-riding-a-bicycle` 博客标签。
- 可复用部分：**提示词语义**（一句功能性指令，本身可版权性极低）+「一个模型一个产物、README 平铺 `<img width="600">`」的展示范式。
- 侵入成本：`generate-svgs.sh` 依赖 pip 安装 `llm` CLI，与 Go 技术栈不兼容；且无 License → 拷贝即侵权。
- 结论：**参考**。产品里若提及基准来源，用自写描述 + 外链，不复制其原文段落与示例 SVG。

**#2 delirious6423/pelican-bench — 复用正则表（MIT）**

- 一手证据：`harness/pelican.py`（5041 B）中 `_FLAGS` 表原文（MIT，可抄）：

```python
_FLAGS = {
    "script": re.compile(r"<script\b", re.I),
    "event_handler": re.compile(r"\son[a-z]+\s*=", re.I),
    "foreign_object": re.compile(r"<foreignObject\b", re.I),
    "javascript_url": re.compile(r"javascript\s*:", re.I),
    "entity_decl": re.compile(r"<!ENTITY", re.I),
    "doctype": re.compile(r"<!DOCTYPE", re.I),
    "embed_object_iframe": re.compile(r"<(iframe|object|embed)\b", re.I),
    "css_import": re.compile(r"@import", re.I),
    "data_uri": re.compile(r"""(?:href|src)\s*=\s*["']\s*data:""", re.I),
}
```

- 可复用部分：**这 9 类危险构造**（脚本、事件属性、foreignObject、javascript:、ENTITY、DOCTYPE、iframe/object/embed、CSS @import、data: URI）→ Go 侧改写成 `regexp` 表，作为「入库前静态扫描 + 打标」。
- 不可复用：`bubblewrap` + `librsvg` 沙箱渲染链路（Python + 系统依赖），命中「不得引入第三方运行时」红线。
- 侵入成本：低（正则改写约 40 行 + 单测）。
- 结论：**复用（MIT，保留版权声明于 `THIRD_PARTY_NOTICES.md`）**。

**#3 joeseesun/llm-case-benchmark — 复用分类器逻辑（MIT）**

- 一手证据：`public/output-classifier.js`（8406 B）含 `MIN_ARTIFACT_COVERAGE = 0.72`、`matchCompleteSvg`、`matchRecoverableSvg`、`classifyModelOutput`、`hasStrongArtifactIntent`、`decodeArtifactEntities` 等纯函数。
- 可复用部分：**「模型输出 → 渲染形态」判定算法**（完整/可恢复 SVG、HTML、代码围栏、Markdown、文本，以及「产出覆盖率 ≥ 0.72 才算主导产物」的阈值）。我们 Go 侧需要同一套判定来填 `artifacts.render_kind`，因此**重写为 Go**，不直接 import UMD JS 进 TS 工程。
- 不可复用：`server.js`（Node/Express + SQLite + bcrypt 管理员体系，57 KB）= 独立 Node 服务，红线禁止；其「运行 → 检查 → 发布为不可变 v1/v2」的**交互语义**可参考，但我们的发布模型是「按 run 落库 + 清理策略」，不需要独立发布版本号。
- 侵入成本：低-中（判定逻辑重写 + 表结构对齐）。
- 结论：**复用逻辑（MIT），保留版权声明**。

**#4 johnbean393/SVGBench / #15 compare-llms / #16 img-comp-slider — 放弃**

- 三者 API `license: null`，LICENSE 文件缺失或 404。仅可参考「盲评画廊 / 并排对比滑杆」的交互思路，**代码与题库 JSON 一律不拷**。
- SVGBench 额外依赖 ChromeDriver 做栅格化，与我们「不做服务端栅格化」的决策冲突。

**#5 lmring — 参考设计**

- Apache-2.0，License 干净。但 Next.js 16 + React 19 + Drizzle + PostgreSQL 全栈单体，框架与我们的 Go+Vue 完全不同。可参考：Arena 多模型并排卡片布局、Leaderboard 三视图（表格/柱状/散点）切换。
- 结论：**仅参考 UI 设计**，不引入依赖。

**#6~#10 评测框架 — 放弃**

- arena-hard-auto / OpenCompass / lm-evaluation-harness / FastChat / SVGenius 全部是 **Python 服务或本地评测 CLI**，License 虽干净，但命中「不得引入 Python 服务、独立运行时」红线。可参考的仅是 Elo / Bradley-Terry 排序与题目集组织方式 —— 而本功能首版评分以「人工 + 规则」为主，LLM 裁判可选且可关闭，不引入排名算法。
- 结论：**放弃**。

**#11 DOMPurify — 已内置**

- Apache-2.0，`frontend/package.json` 已含 `dompurify ^3.4.17`，且 `frontend/src/utils/sanitize.ts` 已封装：

```ts
export function sanitizeSvg(svg: string): string {
  if (!svg) return ''
  return DOMPurify.sanitize(svg, { USE_PROFILES: { svg: true, svgFilters: true } })
}
```

- 结论：**直接复用现有封装**，本功能不新增依赖，只在 feature 目录内补充更严格的 profile 与单测。

**#12~#14 前端组件库 — 不引入**

- Element Plus / splitpanes / vue-easy-lightbox 均 MIT 且可用，但项目当前**未使用任何 UI 组件库**（仅 Tailwind + 自研组件），为单一功能引入 Element Plus（27k★ 但体积大）会改变整体依赖面；vue-easy-lightbox 最后提交 2024-04，维护停滞。
- 结论：**自研网格/灯箱**（Tailwind + 原生 `<dialog>` / `<img>`），零新增依赖。

---

## 3. 「pelican riding a bicycle」基准的出处与可复用资产

### 3.1 出处与运作方式

- 原始仓库：`https://github.com/simonw/pelican-bicycle`，README 首段即写明基准用途与外链。
- 提示词：`Generate an SVG of a pelican riding a bicycle`（一句话自然语言指令）。
- 该基准的运作方式是「**每有新模型就同题重跑一次**」，这与我们「分组 × 模型 定期跑同一道题」的调度模型完全同构，因此**领域模型可直接照搬**（Task = 分组+模型+题目集合+间隔；Run = 一次批跑；Artifact = 单模型单产出）。

### 3.2 可复用资产与 License 判定

| 资产 | 来源 | License | 动作 |
|------|------|---------|------|
| 默认题目文案（鹈鹕骑自行车） | 自写中文题面 + 自写英文提示词（语义参照公共基准，不复制原文段落） | 无版权负担 | 直接使用 |
| SVG 危险构造正则表（9 类） | pelican-bench `harness/pelican.py` `_FLAGS` | MIT | **抄代码** → Go 正则，保留版权声明 |
| 输出形态判定（完整/可恢复 SVG/HTML/Markdown + 覆盖率阈值） | llm-case-benchmark `public/output-classifier.js` | MIT | **重写为 Go**，保留版权声明 |
| 前端 SVG 消毒 | 项目已有 `sanitizeSvg`（DOMPurify） | Apache-2.0 | 复用现有封装 |
| 图片网格 / 灯箱 / 对比视图 | 无（自研） | — | 自研 |
| 盲评（隐藏模型名 + 打分）交互 | pelican-bench / lmring | MIT / Apache-2.0 | 参考，首版不做 |

### 3.3 SVG 输出渲染的安全注意（必须落地）

1. **禁止 `v-html` 直出未消毒内容**。SVG 可嵌 `<script>`、`onload=`、`<foreignObject>`（内放任意 HTML）、`javascript:` href、`<!ENTITY>`（XXE/实体炸弹）、`<iframe|object|embed>`、CSS `@import` 与外部 `url()`（信标泄露用户 IP）。
2. **双保险**：Go 侧入库前静态扫描并打标（命中即 `blocked` 或强制降级为纯文本展示）；前端渲染前再过 `sanitizeSvg`。
3. **优先 `<img src>` 路径**：`image_url` 形态走对象存储直链 + `<img>`，SVG 作为图片源时脚本不执行；仅当必须内联时才使用消毒后的内联 SVG。
4. **不做服务端栅格化**：避免引入 librsvg / ChromeDriver / 无头浏览器等重型依赖。

---

## 4. 与主项目 License 兼容性

- 主项目 LICENSE = LGPL-3.0（见仓库根 `LICENSE`）。
- MIT / Apache-2.0 / BSD / ISC 与 LGPL-3.0 兼容，可复用，需保留版权声明与 License 全文。
- 落地方式：新建 `backend/internal/iqbenchmark/THIRD_PARTY_NOTICES.md`，登记 pelican-bench（MIT）与 llm-case-benchmark（MIT）的版权行。
- Apache-2.0 的 DOMPurify 以 **npm 依赖**形式存在（不修改源码、不并入同一编译单元），沿用现状即可。

---

## 5. 未核实项与风险提示

1. **未核实**：`simonwillison.net` 全站是否另有 CC 授权条款。已抓取 `simonw/pelican-bicycle` README 与仓库内容；站点页面未逐一核证。因此博文正文、示例 SVG 一律按「保留所有权利」对待。
2. **未核实**：`johnbean393/SVGBench` 是否曾有 LICENSE 后被删除（本次仅核证 `main` 分支根目录与 API 字段）。
3. **风险**：模型输出可能含不当内容或内嵌追踪；作品展公开前需服务端消毒 + 静态扫描，且 SVG 不应以 `image/svg+xml` 同源直出。
4. **风险**：定期批量检测持续消耗额度；必须默认关闭 + 日成本预算上限 + 配额耗尽自动暂停（已在 `design.md` 覆盖）。
5. **风险**：pelican-bench 实测部分模型单次生成耗时可达数分钟、输出 token 可达 3 万级 → 必须有超时与响应体上限，超限截断并记为失败作品。

---

## 6. 最终取舍

| 决策 | 结论 |
|------|------|
| 是否复用整套实现 | **否**。无同栈（Go+Vue）实现；高同构候选均为 Python/Node 独立服务。 |
| 是否复用片段代码 | **是**，仅两处 MIT 片段：SVG 危险构造正则表、输出形态判定算法（均为重写/改写，保留版权声明）。 |
| 是否引入新依赖 | **否**（前端复用已有 DOMPurify；后端仅用标准库 + 已有 `golang.org/x/net`，不新增第三方库）。 |
| 总体路线 | **参考设计 + 自研实现**。 |
