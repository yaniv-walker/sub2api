<template>
  <section class="space-y-3" aria-label="教程编辑器">
    <div
      class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-200 pb-3 dark:border-dark-700"
    >
      <div class="flex gap-1">
        <button
          v-for="tab in modes"
          :key="tab.value"
          type="button"
          class="btn btn-sm"
          :class="mode === tab.value ? 'btn-primary' : 'btn-secondary'"
          @click="switchMode(tab.value)"
        >
          {{ tab.label }}
        </button>
      </div>
      <span class="text-xs text-gray-500"
        >截图可粘贴或拖入 · 图片最大 20 MiB，视频最大 200 MiB</span
      >
    </div>
    <p
      v-if="warning"
      class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
      role="alert"
    >
      {{ warning }}
    </p>
    <div
      v-if="mode === 'visual' && editor"
      class="flex flex-wrap gap-1"
      aria-label="正文格式"
    >
      <button
        v-for="tool in tools"
        :key="tool.label"
        type="button"
        class="btn btn-secondary btn-sm"
        :title="tool.label"
        @click="tool.run"
      >
        {{ tool.label }}
      </button>
      <select
        class="input !w-auto !py-1 text-xs"
        aria-label="文字颜色"
        @change="
          editor
            .chain()
            .focus()
            .setColor(($event.target as HTMLSelectElement).value)
            .run()
        "
      >
        <option value="">文字颜色</option>
        <option v-for="color in colors" :key="color" :value="color">
          {{ color }}
        </option>
      </select>
      <select
        class="input !w-auto !py-1 text-xs"
        aria-label="文字高亮"
        @change="
          editor
            .chain()
            .focus()
            .toggleHighlight({
              color: ($event.target as HTMLSelectElement).value,
            })
            .run()
        "
      >
        <option value="">文字高亮</option>
        <option v-for="color in highlights" :key="color" :value="color">
          {{ color }}
        </option>
      </select>
      <select
        class="input !w-auto !py-1 text-xs"
        aria-label="图片宽度"
        @change="
          editor
            .chain()
            .focus()
            .updateAttributes('image', {
              width: ($event.target as HTMLSelectElement).value,
            })
            .run()
        "
      >
        <option value="100%">图片宽度</option>
        <option v-for="width in widths" :key="width" :value="width">
          {{ width }}
        </option>
      </select>
      <select
        class="input !w-auto !py-1 text-xs"
        aria-label="提示框"
        @change="setCallout(($event.target as HTMLSelectElement).value)"
      >
        <option value="">提示框</option>
        <option v-for="tone in tones" :key="tone" :value="tone">
          {{
            { cyan: "信息", violet: "重点", green: "提示", rose: "注意" }[tone]
          }}
        </option>
      </select>
    </div>
    <div @dragover.prevent @drop.prevent="dropFiles">
      <EditorContent
        v-if="mode === 'visual'"
        :editor="editor"
        class="tutorial-content rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900"
      />
      <textarea
        v-else-if="mode === 'source'"
        ref="source"
        :value="modelValue"
        class="input min-h-[32rem] font-mono text-sm leading-7"
        aria-label="Markdown 正文"
        spellcheck="false"
        @input="
          emit(
            'update:modelValue',
            ($event.target as HTMLTextAreaElement).value,
          )
        "
        @paste="pasteSource"
      ></textarea>
      <TutorialContent
        v-else
        :markdown="previewMarkdown"
        @retry="loadAssets"
        class="min-h-[24rem] p-4"
      />
    </div>
    <p v-if="uploading" class="text-sm text-primary-600" role="status">
      正在上传 {{ uploadName }} · {{ progress }}%
    </p>
    <p v-if="error" class="text-sm text-red-600" role="alert">{{ error }}</p>
    <div class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="!tutorialId || uploading"
        @click="fileInput?.click()"
      >
        上传图片、视频或文件</button
      ><button
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="!tutorialId"
        @click="
          tray = !tray;
          loadAssets();
        "
      >
        {{ tray ? "收起素材库" : `素材库（${assets.length}）` }}</button
      ><span v-if="!tutorialId" class="text-xs text-gray-500"
        >先保存草稿，再上传素材。</span
      ><input
        ref="fileInput"
        type="file"
        class="hidden"
        multiple
        accept=".jpg,.jpeg,.png,.gif,.webp,.mp4,.webm,.pdf,.txt,.zip"
        @change="chosenFiles"
      />
    </div>
    <section
      v-if="tray"
      class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"
      aria-label="素材库"
    >
      <p class="mb-3 text-xs text-gray-500">
        替换只修改当前草稿；发布前，读者仍看到旧素材。使用中的素材不可删除。
      </p>
      <p v-if="!assets.length" class="py-5 text-center text-sm text-gray-500">
        还没有素材。上传后可以插入正文。
      </p>
      <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        <article
          v-for="asset in assets"
          :key="asset.id"
          class="min-w-0 rounded-lg border border-gray-200 p-3 dark:border-dark-700"
        >
          <img
            v-if="asset.kind === 'image'"
            :src="asset.preview_url || asset.public_url"
            :alt="asset.label"
            class="mb-2 h-24 w-full object-contain"
          /><video
            v-else-if="asset.kind === 'video'"
            :src="asset.preview_url || asset.public_url"
            controls
            preload="metadata"
            class="mb-2 h-24 w-full"
          ></video>
          <p
            v-else
            class="mb-2 grid h-24 place-items-center bg-gray-50 text-gray-500 dark:bg-dark-900"
          >
            下载文件
          </p>
          <label class="text-xs text-gray-500"
            >素材名称<input
              v-model="asset.label"
              class="input mt-1 !py-1 text-xs"
              @change="rename(asset)"
          /></label>
          <p class="mt-2 truncate text-xs text-gray-500" :title="asset.sha256">
            {{ asset.original_name }} ·
            {{ (asset.size_bytes / 1024).toFixed(1) }} KiB
          </p>
          <p v-if="asset.references?.length" class="mt-1 text-xs text-gray-500">
            引用：{{ asset.references.map((x) => x.title).join("、") }}
          </p>
          <div class="mt-3 flex flex-wrap gap-2">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              @click="insertAsset(asset)"
            >
              插入</button
            ><button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="uploading"
              @click="
                replacement = asset;
                replacementInput?.click();
              "
            >
              替换</button
            ><button
              type="button"
              class="btn btn-secondary btn-sm text-red-600"
              @click="removeAsset(asset)"
            >
              删除
            </button>
          </div>
        </article>
      </div>
      <input
        ref="replacementInput"
        type="file"
        class="hidden"
        accept=".jpg,.jpeg,.png,.gif,.webp,.mp4,.webm,.pdf,.txt,.zip"
        @change="replaceChosen"
      />
    </section>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Editor, EditorContent } from "@tiptap/vue-3";
import { Node, mergeAttributes } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Image from "@tiptap/extension-image";
import { TextStyle } from "@tiptap/extension-text-style";
import Color from "@tiptap/extension-color";
import Highlight from "@tiptap/extension-highlight";
import {
  deleteTutorialAsset,
  listTutorialAssets,
  renameTutorialAsset,
  uploadTutorialAsset,
  type TutorialAsset,
} from "@/api/tutorial";
import TutorialContent from "./TutorialContent.vue";
import {
  colors,
  canonicalAssetHTML,
  highlights,
  tones,
  widths,
  errorMessage,
  htmlToMarkdown,
  renderTutorial,
  replaceAssetReference,
  unsupportedVisualContent,
} from "./content";
const props = defineProps<{ modelValue: string; tutorialId?: number; documentVersion?: number }>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
type Mode = "visual" | "source" | "preview";
const modes: { value: Mode; label: string }[] = [
  { value: "visual", label: "可视化" },
  { value: "source", label: "Markdown" },
  { value: "preview", label: "预览" },
];
const mode = ref<Mode>("source");
const warning = ref("");
const error = ref("");
const assets = ref<TutorialAsset[]>([]);
const tray = ref(false);
const uploading = ref(false);
const uploadName = ref("");
const progress = ref(0);
const fileInput = ref<HTMLInputElement>();
const replacementInput = ref<HTMLInputElement>();
const source = ref<HTMLTextAreaElement>();
const replacement = ref<TutorialAsset>();
const editor = ref<Editor>();
let lastValue = "";
const refreshMediaViews = new Set<() => void>();
let assetRequest = 0;
const previewMarkdown = computed(() =>
  assets.value.reduce(
    (value, asset) =>
      replaceAssetReference(
        value,
        asset.public_url,
        asset.preview_url || asset.public_url,
      ),
    props.modelValue,
  ),
);
function previewURL(url: string) {
  return assets.value.find((x) => x.public_url === url)?.preview_url || url;
}
function mediaView(tag: "img" | "video") {
  return ({ node }: { node: { attrs: Record<string, string> } }) => {
    const dom = document.createElement(tag);
    Object.entries(node.attrs).forEach(([key, value]) => {
      if (value && key !== "width") dom.setAttribute(key, value);
    });
    const refreshPreview = () => {
      const src = previewURL(node.attrs.src || "");
      if (dom.getAttribute("src") !== src) dom.setAttribute("src", src);
      if (tag === "video" && node.attrs.poster) {
        const poster = previewURL(node.attrs.poster);
        if (dom.getAttribute("poster") !== poster) dom.setAttribute("poster", poster);
      }
    };
    refreshPreview();
    refreshMediaViews.add(refreshPreview);
    if (tag === "video") {
      dom.setAttribute("controls", "");
      dom.setAttribute("preload", "metadata");
    } else dom.setAttribute("data-width", node.attrs.width || "100%");
    return {
      dom,
      // Preview credentials belong only to the node view, never to the document.
      ignoreMutation: () => true,
      destroy: () => { refreshMediaViews.delete(refreshPreview); },
    };
  };
}
const SizedImage = Image.extend({
  addAttributes() {
    return {
      ...this.parent?.(),
      width: {
        default: "100%",
        parseHTML: (el) => el.getAttribute("data-width") || "100%",
        renderHTML: (attrs) => ({ "data-width": attrs.width }),
      },
    };
  },
  addNodeView() {
    return mediaView("img");
  },
});
const Video = Node.create({
  name: "tutorialVideo",
  group: "block",
  atom: true,
  draggable: true,
  addAttributes: () => ({ src: { default: "" }, poster: { default: "" } }),
  parseHTML: () => [{ tag: "video[src]" }],
  renderHTML: ({ HTMLAttributes }) => [
    "video",
    mergeAttributes(
      { controls: true, preload: "metadata", playsinline: true },
      HTMLAttributes,
    ),
  ],
  addNodeView() {
    return mediaView("video");
  },
});
const Callout = Node.create({
  name: "tutorialCallout",
  group: "block",
  content: "block+",
  defining: true,
  addAttributes: () => ({
    tone: {
      default: "cyan",
      parseHTML: (el) => el.getAttribute("data-callout"),
      renderHTML: (attrs) => ({ "data-callout": attrs.tone }),
    },
  }),
  parseHTML: () => [{ tag: "div[data-callout]" }],
  renderHTML: ({ HTMLAttributes }) => ["div", HTMLAttributes, 0],
});
function createEditor() {
  editor.value?.destroy();
  editor.value = new Editor({
    extensions: [
      StarterKit.configure({
        heading: { levels: [1, 2, 3, 4] },
        link: { openOnClick: false },
      }),
      SizedImage,
      Video,
      Callout,
      TextStyle,
      Color,
      Highlight.configure({ multicolor: true }),
    ],
    content: renderTutorial(props.modelValue).html,
    editorProps: {
      transformPastedHTML: canonicalAssetHTML,
      attributes: {
        class: "min-h-[32rem] p-5 outline-none",
        "aria-label": "可视化正文",
      },
      handlePaste: (_view, event) => {
        const files = [...(event.clipboardData?.files || [])];
        if (files.length) {
          void uploadFiles(files);
          return true;
        }
        return false;
      },
      handleDrop: (_view, event, _slice, moved) => {
        if (!moved && event.dataTransfer?.files.length) {
          event.preventDefault();
          void uploadFiles([...event.dataTransfer.files]);
          return true;
        }
        return false;
      },
    },
    onUpdate: ({ editor: value }) => {
      lastValue = htmlToMarkdown(value.getHTML());
      emit("update:modelValue", lastValue);
    },
  });
}
function switchMode(next: Mode) {
  warning.value = "";
  if (next === "visual") {
    const unsupported = unsupportedVisualContent(
      renderTutorial(props.modelValue).html,
    );
    if (unsupported.length) {
      warning.value = `正文包含可视化编辑器暂不支持的格式（${unsupported.join("、")}），请使用 Markdown 模式编辑，原文会完整保留。`;
      return;
    }
    createEditor();
  }
  mode.value = next;
}
const tools = [
  {
    label: "标题",
    run: () => editor.value?.chain().focus().toggleHeading({ level: 2 }).run(),
  },
  {
    label: "粗体",
    run: () => editor.value?.chain().focus().toggleBold().run(),
  },
  {
    label: "斜体",
    run: () => editor.value?.chain().focus().toggleItalic().run(),
  },
  {
    label: "列表",
    run: () => editor.value?.chain().focus().toggleBulletList().run(),
  },
  {
    label: "步骤",
    run: () => editor.value?.chain().focus().toggleOrderedList().run(),
  },
  {
    label: "引用",
    run: () => editor.value?.chain().focus().toggleBlockquote().run(),
  },
  {
    label: "代码",
    run: () => editor.value?.chain().focus().toggleCodeBlock().run(),
  },
  {
    label: "链接",
    run: () => {
      const url = prompt(
        "链接地址（HTTPS 或本站路径）",
        editor.value?.getAttributes("link").href || "https://",
      );
      if (url === null) return;
      if (!url) editor.value?.chain().focus().unsetLink().run();
      else if (/^(https?:\/\/|\/(?!\/))/.test(url))
        editor.value?.chain().focus().setLink({ href: url }).run();
      else error.value = "请输入有效链接。";
    },
  },
  { label: "撤销", run: () => editor.value?.chain().focus().undo().run() },
  { label: "重做", run: () => editor.value?.chain().focus().redo().run() },
];
function setCallout(tone: string) {
  if (!editor.value || !tone) return;
  if (editor.value.isActive("tutorialCallout"))
    editor.value
      .chain()
      .focus()
      .updateAttributes("tutorialCallout", { tone })
      .run();
  else editor.value.chain().focus().wrapIn("tutorialCallout", { tone }).run();
}
async function loadAssets() {
  if (!props.tutorialId) return;
  const id = props.tutorialId;
  const request = ++assetRequest;
  try {
    const result = await listTutorialAssets(id);
    if (request !== assetRequest || props.tutorialId !== id) return;
    assets.value = result;
    refreshMediaViews.forEach(refresh => refresh());
  } catch (e) {
    if (request === assetRequest) error.value = errorMessage(e, "素材加载失败");
  }
}
function insertText(text: string) {
  const el = source.value;
  const start = el?.selectionStart ?? props.modelValue.length;
  const end = el?.selectionEnd ?? start;
  emit(
    "update:modelValue",
    props.modelValue.slice(0, start) + text + props.modelValue.slice(end),
  );
  el?.focus();
}
function insertAsset(asset: TutorialAsset) {
  if (mode.value === "visual" && editor.value) {
    const node =
      asset.kind === "image"
        ? { type: "image", attrs: { src: asset.public_url, alt: asset.label } }
        : asset.kind === "video"
          ? { type: "tutorialVideo", attrs: { src: asset.public_url } }
          : {
              type: "paragraph",
              content: [
                {
                  type: "text",
                  text: asset.label,
                  marks: [{ type: "link", attrs: { href: asset.public_url } }],
                },
              ],
            };
    editor.value.chain().focus().insertContent(node).run();
  } else {
    if (mode.value === "preview") mode.value = "source";
    const title = asset.label.replace(/[\[\]<>]/g, "");
    insertText(
      asset.kind === "image"
        ? `\n\n![${title}](${asset.public_url})\n\n`
        : asset.kind === "video"
          ? `\n\n<video controls preload="metadata" playsinline src="${asset.public_url}"></video>\n\n`
          : `\n[下载 ${title}](${asset.public_url})\n`,
    );
  }
}
async function uploadFiles(files: File[]) {
  if (!props.tutorialId) {
    error.value = "请先保存草稿，再上传素材。";
    return;
  }
  if (uploading.value) return;
  uploading.value = true;
  error.value = "";
  try {
    for (const file of files) {
      uploadName.value = file.name;
      progress.value = 0;
      try {
        const asset = await uploadTutorialAsset(
          props.tutorialId,
          file,
          (value) => {
            progress.value = value;
          },
        );
        assets.value.push(asset);
        insertAsset(asset);
      } catch (e) {
        error.value = errorMessage(
          e,
          `${file.name} 上传失败，请重新选择文件重试。`,
        );
      }
    }
  } finally {
    uploading.value = false;
  }
}
function chosenFiles(event: Event) {
  const target = event.target as HTMLInputElement;
  void uploadFiles([...(target.files || [])]);
  target.value = "";
}
function dropFiles(event: DragEvent) {
  if (mode.value !== "visual" && event.dataTransfer?.files.length)
    void uploadFiles([...event.dataTransfer.files]);
}
function pasteSource(event: ClipboardEvent) {
  if (event.clipboardData?.files.length) {
    event.preventDefault();
    void uploadFiles([...event.clipboardData.files]);
  }
}
async function rename(asset: TutorialAsset) {
  try {
    Object.assign(
      asset,
      await renameTutorialAsset(props.tutorialId!, asset.id, asset.label),
    );
  } catch (e) {
    error.value = errorMessage(e, "重命名失败");
  }
}
async function removeAsset(asset: TutorialAsset) {
  if (!confirm(`确认删除素材“${asset.label}”？使用中的素材会被保留。`)) return;
  try {
    await deleteTutorialAsset(props.tutorialId!, asset.id);
    await loadAssets();
  } catch (e) {
    error.value = errorMessage(e, "删除失败");
  }
}
async function replaceChosen(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  const old = replacement.value;
  input.value = "";
  if (!file || !old || !props.tutorialId) return;
  uploading.value = true;
  error.value = "";
  uploadName.value = file.name;
  try {
    const asset = await uploadTutorialAsset(
      props.tutorialId,
      file,
      (value) => {
        progress.value = value;
      },
      old.id,
    );
    assets.value.push(asset);
    emit(
      "update:modelValue",
      replaceAssetReference(props.modelValue, old.public_url, asset.public_url),
    );
    warning.value = "素材已替换到当前草稿，请保存并发布后生效。";
    await loadAssets();
  } catch (e) {
    error.value = errorMessage(e, "替换失败");
  } finally {
    uploading.value = false;
  }
}
watch(
  () => props.modelValue,
  (value) => {
    if (editor.value && value !== lastValue && mode.value === "visual")
      editor.value.commands.setContent(renderTutorial(value).html, {
        emitUpdate: false,
      });
  },
);
watch(
  () => props.tutorialId,
  async () => {
    editor.value?.destroy();
    editor.value = undefined;
    mode.value = "source";
    warning.value = "";
    await loadAssets();
    if (!unsupportedVisualContent(renderTutorial(props.modelValue).html).length)
      switchMode("visual");
  },
  { immediate: true },
);
watch(() => props.documentVersion, () => { void loadAssets(); });
onBeforeUnmount(() => { assetRequest++; editor.value?.destroy(); refreshMediaViews.clear(); });
</script>
