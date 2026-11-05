import { describe, expect, it } from "vitest";
import {
  htmlToMarkdown,
  renderTutorial,
  replaceAssetReference,
  unsupportedVisualContent,
} from "../content";

describe("tutorial content round trip", () => {
  it("removes temporary local media credentials when pasting rendered HTML", () => {
    const markdown = htmlToMarkdown('<img src="/api/v1/tutorial-assets/image?sig=secret&amp;expires=123"><video src="/api/v1/tutorial-assets/clip?sig=secret" controls></video>');
    expect(markdown).toContain('/api/v1/tutorial-assets/image');
    expect(markdown).toContain('/api/v1/tutorial-assets/clip');
    expect(markdown).not.toContain('sig=');
  });
  it("preserves media, sizing, callouts, color and highlights through visual editing", () => {
    const input =
      '<h2>接入</h2><div data-callout="rose"><p><span style="color:#dc2626">注意</span><mark style="background-color:#fef08a">密钥</mark></p></div><img src="/api/v1/tutorial-assets/image" alt="截图" data-width="50%"><video controls playsinline preload="metadata" src="/api/v1/tutorial-assets/video"></video>';
    const html = renderTutorial(htmlToMarkdown(input)).html;
    expect(html).toContain('data-callout="rose"');
    expect(html).toContain('data-width="50%"');
    expect(html).toContain("<video");
    expect(html).toContain("playsinline");
    expect(html).toContain("color:");
    expect(html).toContain("<mark");
  });
  it("removes executable content and creates deterministic non-duplicated heading ids", () => {
    const content =
      '## 配置\n\n<script>alert(1)</script><img src="x" onerror="alert(1)">\n\n## 配置';
    const first = renderTutorial(content);
    expect(first.html).not.toContain("<script");
    expect(first.html).not.toContain("onerror");
    expect(new Set(first.toc.map((x) => x.id)).size).toBe(2);
    expect(renderTutorial(content).toc).toEqual(first.toc);
  });
  it("replaces exactly the selected asset without changing similar references", () => {
    const text =
      '![a](/api/v1/tutorial-assets/a)\n<img src="/api/v1/tutorial-assets/ab">';
    expect(
      replaceAssetReference(
        text,
        "/api/v1/tutorial-assets/a",
        "/api/v1/tutorial-assets/new",
      ),
    ).toBe(
      '![a](/api/v1/tutorial-assets/new)\n<img src="/api/v1/tutorial-assets/ab">',
    );
  });
  it("protects unrecognised markup from lossy visual conversion", () => {
    expect(
      unsupportedVisualContent("<table><tr><td>keep me</td></tr></table>"),
    ).toContain("TABLE");
    expect(
      unsupportedVisualContent('<p>ok</p><video src="/clip.mp4"></video>'),
    ).toEqual([]);
  });
});
