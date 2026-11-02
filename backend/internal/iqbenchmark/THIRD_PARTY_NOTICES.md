# 第三方代码归属声明（模型智商检测作品展）

本模块（`backend/internal/iqbenchmark/`）在两处以「改写为 Go」的方式复用了
MIT 许可项目的算法/规则表。原始项目与本声明一起保留，未拷贝任何项目文件。

代码级复用清单与核证过程见 `openspec/changes/add-iq-benchmark-gallery/research.md`。

---

## 1. delirious6423/pelican-bench

- **URL**：https://github.com/delirious6423/pelican-bench
- **许可**：MIT License
- **复用范围**：`harness/pelican.py` 中的 `_FLAGS` 表（9 类 SVG 危险构造正则）
  → 改写为 Go `regexp`，落于 `backend/internal/iqbenchmark/sanitize/svg.go`
  （`reScript` / `reEventHandler` / `reForeignObject` / `reJavaScriptURL` /
  `reEntityDecl` / `reDoctype` / `reEmbedObjectFrame` / `reCSSImport` / `reDataURI`）。
- **未复用**：该仓库的 Python 检测框架、提示词集合、示例产物图片。

```
MIT License

Copyright (c) 2024 delirious6423

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## 2. joeseesun/llm-case-benchmark

- **URL**：https://github.com/joeseesun/llm-case-benchmark
- **许可**：MIT License
- **复用范围**：`public/output-classifier.js` 的输出形态判定算法
  （完整 SVG / 可恢复截断 SVG 的识别、产物覆盖率阈值 `MIN_ARTIFACT_COVERAGE = 0.72`）
  → 改写为 Go，落于 `backend/internal/iqbenchmark/sanitize/classify.go`
  （`Classify` / `dominantFencedBlock` / `classifyFragment` / `coverage` / `ExtractSVG`，
  常量 `MinArtifactCoverage`）。
- **未复用**：该仓库的 Node + Express + SQLite 服务端（`server.js`，约 57KB）、
  前端页面与静态资源。

```
MIT License

Copyright (c) 2025 joeseesun

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## 3. 明确未复用（无 License）

- `simonw/pelican-bicycle`（「pelican riding a bicycle」基准的出处）：
  **仓库无任何 License 声明**（GitHub API `license: null`）。仅作为产品思路参考，
  其代码与 20 个示例 SVG 产物**一律未拷贝、未参考实现细节**。
- `johnbean393/SVGBench`、`bilikaz/compare-llms`、`Yubo0826/img-comp-slider`：
  同样无 License，未复用。
