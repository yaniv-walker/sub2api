<template>
  <div class="tutorial-content" @click="openImage" @error.capture="mediaFailed" v-html="rendered.html"></div>
  <p v-if="mediaError" class="mt-3 rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300" role="status">部分图片或视频未能加载，访问链接可能已到期。<button type="button" class="ml-2 underline" @click="mediaError = false; $emit('retry')">重新加载素材</button></p>
  <Teleport to="body"
    ><div
      v-if="image"
      ref="dialog"
      class="fixed inset-0 z-[100] grid place-items-center bg-black/85 p-6"
      role="dialog"
      aria-modal="true"
      aria-label="图片预览"
      tabindex="-1"
      @click="closeImage"
      @keydown.esc="closeImage"
    >
      <button
        class="absolute right-5 top-5 rounded bg-white px-4 py-2 text-gray-900"
        @click="closeImage"
      >
        关闭</button
      ><img
        :src="image"
        alt="放大预览"
        class="max-h-[90vh] max-w-full object-contain"
      /></div
  ></Teleport>
</template>
<script setup lang="ts">
import { computed, nextTick, ref } from "vue";
import { renderTutorial } from "./content";
const props = defineProps<{ markdown?: string; html?: string }>();
defineEmits<{ retry: [] }>();
const mediaError = ref(false);
function mediaFailed(event: Event) { if (event.target instanceof HTMLImageElement || event.target instanceof HTMLVideoElement) mediaError.value = true; }
const image = ref("");
const dialog = ref<HTMLElement>();
let previousFocus: HTMLElement | null = null;
function closeImage() { image.value = ""; previousFocus?.focus(); }
const rendered = computed(() =>
  renderTutorial(props.markdown || "", props.html),
);
async function openImage(event: MouseEvent) {
  const target = event.target;
  if (target instanceof HTMLImageElement) { previousFocus = document.activeElement as HTMLElement; image.value = target.src; await nextTick(); dialog.value?.focus(); }
}
</script>
<style>
.tutorial-content {
  overflow-wrap: anywhere;
  color: inherit;
  line-height: 1.85;
}
.tutorial-content h1,
.tutorial-content h2,
.tutorial-content h3,
.tutorial-content h4 {
  font-weight: 650;
  line-height: 1.4;
  scroll-margin-top: 6rem;
  margin: 1.5rem 0 0.75rem;
}
.tutorial-content h1 {
  font-size: 1.8rem;
}
.tutorial-content h2 {
  font-size: 1.4rem;
}
.tutorial-content h3 {
  font-size: 1.15rem;
}
.tutorial-content h4 {
  font-size: 1rem;
}
.tutorial-content p {
  margin: 0.8rem 0;
}
.tutorial-content ul {
  list-style: disc;
  padding-left: 1.5rem;
}
.tutorial-content ol {
  list-style: decimal;
  padding-left: 1.5rem;
}
.tutorial-content li {
  margin: 0.3rem 0;
}
.tutorial-content a {
  color: #0284c7;
  text-decoration: underline;
}
.tutorial-content img {
  display: block;
  max-width: 100%;
  height: auto;
  margin: 1rem auto;
  border-radius: 0.375rem;
  cursor: zoom-in;
}
.tutorial-content img[data-width="25%"] {
  width: 25%;
}
.tutorial-content img[data-width="50%"] {
  width: 50%;
}
.tutorial-content img[data-width="75%"] {
  width: 75%;
}
.tutorial-content img[data-width="100%"] {
  width: 100%;
}
.tutorial-content video {
  display: block;
  width: 100%;
  max-height: 40rem;
  margin: 1rem auto;
  border-radius: 0.375rem;
  background: #0f172a;
}
.tutorial-content blockquote {
  border-left: 3px solid #94a3b8;
  padding: 0.25rem 1rem;
  color: #64748b;
}
.tutorial-content pre {
  overflow-x: auto;
  border-radius: 0.5rem;
  padding: 1rem;
  background: #0f172a;
  color: #e2e8f0;
}
.tutorial-content code {
  font-family: ui-monospace, monospace;
  font-size: 0.9em;
}
.tutorial-content :not(pre) > code {
  background: #94a3b822;
  border-radius: 0.2rem;
  padding: 0.1rem 0.3rem;
}
.tutorial-content hr {
  margin: 1.5rem 0;
  border-color: #cbd5e1;
}
.tutorial-content table {
  display: block;
  overflow-x: auto;
  border-collapse: collapse;
}
.tutorial-content td,
.tutorial-content th {
  border: 1px solid #cbd5e1;
  padding: 0.5rem 0.8rem;
}
.tutorial-content [data-callout] {
  border: 1px solid #a5d8e6;
  border-radius: 0.375rem;
  padding: 0.75rem 1rem;
  margin: 1rem 0;
  background: #ecfeff;
  color: #164e63;
}
.tutorial-content [data-callout="violet"] {
  border-color: #c4b5fd;
  background: #f5f3ff;
  color: #4c1d95;
}
.tutorial-content [data-callout="green"] {
  border-color: #86efac;
  background: #f0fdf4;
  color: #14532d;
}
.tutorial-content [data-callout="rose"] {
  border-color: #fda4af;
  background: #fff1f2;
  color: #881337;
}
.dark .tutorial-content [data-callout] {
  background: #164e6333;
  color: #cffafe;
}
.dark .tutorial-content [data-callout="violet"] {
  background: #4c1d9533;
  color: #ede9fe;
}
.dark .tutorial-content [data-callout="green"] {
  background: #14532d33;
  color: #dcfce7;
}
.dark .tutorial-content [data-callout="rose"] {
  background: #88133733;
  color: #ffe4e6;
}
@media (max-width: 640px) {
  .tutorial-content img[data-width] {
    min-width: 50%;
  }
  .tutorial-content h1 {
    font-size: 1.5rem;
  }
}
</style>
