import DOMPurify from "dompurify";
import { marked } from "marked";
import TurndownService from "turndown";

export const tutorialCategories = [
  { value: "getting-started", label: "开始使用" },
  { value: "clients", label: "客户端接入" },
  { value: "billing", label: "充值与订阅" },
  { value: "troubleshooting", label: "问题排查" },
];
export const categoryLabel = (value: string) =>
  tutorialCategories.find((x) => x.value === value)?.label || value;
export const statusLabel = (value: string) =>
  ({ draft: "草稿", published: "已发布", offline: "已下线" })[value] || value;
export const errorMessage = (error: unknown, fallback: string) =>
  (error as { status?: number; message?: string })?.status === 409
    ? "内容已被更新或素材仍被引用，请重新加载后检查。未保存的内容仍保留在编辑器中。"
    : (error as { message?: string })?.message || fallback;
export interface TutorialHeading {
  id: string;
  text: string;
  level: number;
}
const widths = ["25%", "50%", "75%", "100%"];
const tones = ["cyan", "violet", "green", "rose"];
const colors = [
  "#475569",
  "#dc2626",
  "#c2410c",
  "#a16207",
  "#15803d",
  "#2563eb",
  "#7c3aed",
];
const highlights = [
  "#fef08a",
  "#fed7aa",
  "#bbf7d0",
  "#bfdbfe",
  "#fbcfe8",
  "#e2e8f0",
];
export { colors, highlights, tones, widths };
function permittedColor(value: string, values: string[]) {
  const element = document.createElement("span");
  element.style.color = value;
  const normalized = element.style.color;
  return (
    values.find((x) => {
      element.style.color = x;
      return normalized && element.style.color === normalized;
    }) || ""
  );
}
export function sanitizeTutorial(html: string): string {
  const template = document.createElement("template");
  template.innerHTML = DOMPurify.sanitize(html, {
    ADD_TAGS: ["video", "source", "mark"],
    ADD_ATTR: [
      "controls",
      "preload",
      "playsinline",
      "poster",
      "data-width",
      "data-callout",
      "style",
    ],
    FORBID_TAGS: ["iframe", "object", "embed", "form"],
  });
  template.content.querySelectorAll<HTMLElement>("[style]").forEach((el) => {
    const color =
      el.tagName === "SPAN" ? permittedColor(el.style.color, colors) : "";
    const highlight =
      el.tagName === "MARK"
        ? permittedColor(el.style.backgroundColor, highlights)
        : "";
    el.removeAttribute("style");
    if (color) el.style.color = color;
    if (highlight) el.style.backgroundColor = highlight;
  });
  template.content
    .querySelectorAll("[data-width]")
    .forEach((el) =>
      el.tagName === "IMG"
        ? el.setAttribute(
            "data-width",
            widths.includes(el.getAttribute("data-width") || "")
              ? el.getAttribute("data-width")!
              : "100%",
          )
        : el.removeAttribute("data-width"),
    );
  template.content
    .querySelectorAll("[data-callout]")
    .forEach((el) =>
      el.tagName === "DIV"
        ? el.setAttribute(
            "data-callout",
            tones.includes(el.getAttribute("data-callout") || "")
              ? el.getAttribute("data-callout")!
              : "cyan",
          )
        : el.removeAttribute("data-callout"),
    );
  template.content.querySelectorAll("a[href]").forEach((el) => {
    el.setAttribute("target", "_blank");
    el.setAttribute("rel", "noopener noreferrer");
  });
  return template.innerHTML;
}
export function renderTutorial(
  markdown: string,
  html?: string,
): { html: string; toc: TutorialHeading[] } {
  const template = document.createElement("template");
  template.innerHTML = sanitizeTutorial(
    html ?? (marked.parse(markdown || "") as string),
  );
  const toc: TutorialHeading[] = [];
  template.content.querySelectorAll("h1,h2,h3,h4").forEach((el, index) => {
    const id = `tutorial-heading-${index}`;
    el.id = id;
    toc.push({ id, text: el.textContent || "", level: Number(el.tagName[1]) });
  });
  return { html: template.innerHTML, toc };
}
export function canonicalAssetHTML(html: string): string {
  const template = document.createElement("template");
  template.innerHTML = html;
  template.content.querySelectorAll("[src],[href],[poster]").forEach(element => {
    for (const attribute of ["src", "href", "poster"]) {
      const value = element.getAttribute(attribute);
      if (!value) continue;
      try {
        const url = new URL(value, window.location.origin);
        if (url.origin === window.location.origin && /^\/api\/v1\/tutorial-assets\/[^/]+$/.test(url.pathname)) element.setAttribute(attribute, url.pathname);
      } catch { /* Invalid URLs are removed by the sanitizer. */ }
    }
  });
  return template.innerHTML;
}
export function htmlToMarkdown(html: string): string {
  const converter = new TurndownService({
    headingStyle: "atx",
    codeBlockStyle: "fenced",
    bulletListMarker: "-",
  });
  converter.addRule("mediaAndCallouts", {
    filter: (element) =>
      element.nodeName === "VIDEO" ||
      (element.nodeName === "IMG" && element.hasAttribute("data-width")) ||
      (element.nodeName === "DIV" && element.hasAttribute("data-callout")),
    replacement: (_text, element) =>
      `\n\n${(element as HTMLElement).outerHTML}\n\n`,
  });
  converter.addRule("textDecoration", {
    filter: (element) =>
      element.nodeName === "MARK" ||
      (element.nodeName === "SPAN" && element.hasAttribute("style")),
    replacement: (_text, element) => (element as HTMLElement).outerHTML,
  });
  return converter
    .turndown(canonicalAssetHTML(sanitizeTutorial(html)))
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}
export function replaceAssetReference(
  content: string,
  oldUrl: string,
  nextUrl: string,
): string {
  const escaped = oldUrl.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return content.replace(new RegExp(`${escaped}(?=[\\s"')>]|$)`, "g"), nextUrl);
}
export function unsupportedVisualContent(html: string): string[] {
  const allowed = new Set([
    "P",
    "BR",
    "H1",
    "H2",
    "H3",
    "H4",
    "STRONG",
    "B",
    "EM",
    "I",
    "S",
    "STRIKE",
    "DEL",
    "UL",
    "OL",
    "LI",
    "BLOCKQUOTE",
    "PRE",
    "CODE",
    "HR",
    "A",
    "IMG",
    "VIDEO",
    "SPAN",
    "MARK",
    "DIV",
  ]);
  const template = document.createElement("template");
  template.innerHTML = html;
  return [
    ...new Set(
      [...template.content.querySelectorAll("*")]
        .filter(
          (el) =>
            !allowed.has(el.tagName) ||
            (el.tagName === "DIV" && !el.hasAttribute("data-callout")) ||
            (el.tagName === "VIDEO" && !el.getAttribute("src")),
        )
        .map((el) => el.tagName),
    ),
  ];
}
