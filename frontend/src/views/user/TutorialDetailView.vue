<template>
  <component :is="auth.isAuthenticated ? AppLayout : PublicLayout">
    <div v-if="loading" class="space-y-4" aria-busy="true">
      <div
        class="h-10 w-2/3 animate-pulse rounded bg-gray-100 dark:bg-dark-800"
      ></div>
      <div
        class="h-96 animate-pulse rounded bg-gray-100 dark:bg-dark-800"
      ></div>
    </div>
    <section v-else-if="error" class="card p-8 text-center">
      <h1 class="font-semibold">教程暂时无法加载</h1>
      <p class="mt-2 text-sm text-red-600" role="alert">{{ error }}</p>
      <div class="mt-4 flex justify-center gap-3">
        <button class="btn btn-secondary" @click="load">重试</button
        ><RouterLink to="/tutorial" class="btn btn-secondary"
          >返回教程中心</RouterLink
        >
      </div>
    </section>
    <div v-else-if="tutorial" class="space-y-5">
      <nav class="text-sm text-gray-500">
        <RouterLink to="/tutorial" class="hover:text-primary-600"
          >教程与售后</RouterLink
        ><span class="mx-2">/</span>{{ tutorial.title }}
      </nav>
      <header
        class="flex flex-col justify-between gap-4 border-b border-gray-200 pb-6 dark:border-dark-700 md:flex-row md:items-end"
      >
        <div>
          <p class="text-xs font-medium text-primary-600">
            {{ categoryLabel(tutorial.category) }}
          </p>
          <h1 class="mt-2 text-3xl font-semibold text-gray-900 dark:text-white">
            {{ tutorial.title }}
          </h1>
          <p class="mt-3 max-w-3xl text-sm leading-6 text-gray-500">
            {{ tutorial.summary }}
          </p>
          <p class="mt-3 text-xs text-gray-400">
            更新于
            {{ new Date(tutorial.updated_at).toLocaleDateString("zh-CN") }}
          </p>
        </div>
        <div class="flex shrink-0 gap-2">
          <button class="btn btn-secondary" @click="copyLink">
            {{ copied ? "链接已复制" : "复制链接" }}</button
          ><RouterLink
            v-if="auth.isAdmin"
            :to="`/admin/tutorials?id=${tutorial.id}`"
            class="btn btn-primary"
            >编辑本页</RouterLink
          >
        </div>
      </header>
      <div class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_14rem]">
        <main class="card min-w-0 !p-5 sm:!p-8">
          <section
            v-if="tutorial.resources?.length"
            class="mb-6 space-y-3 border-b border-gray-200 pb-6 dark:border-dark-700"
            aria-label="下载与参考资源"
          >
            <h2 class="text-sm font-semibold">下载与参考资源</h2>
            <div class="grid gap-2 sm:grid-cols-2">
              <a
                v-for="(resource, index) in tutorial.resources"
                :key="index"
                :href="resource.url"
                target="_blank"
                rel="noopener noreferrer"
                class="rounded-lg border border-gray-200 p-3 hover:border-primary-400 dark:border-dark-700"
                ><span class="block text-sm font-medium text-primary-600"
                  >{{ resource.label }} ↗</span
                ><span class="mt-1 block text-xs text-gray-500">{{
                  [resource.version, resource.platform]
                    .filter(Boolean)
                    .join(" · ")
                }}</span
                ><span
                  v-if="resource.sha256"
                  class="mt-1 block break-all font-mono text-[10px] text-gray-500"
                  >SHA256 {{ resource.sha256 }}</span
                ></a
              >
            </div>
          </section>
          <details
            class="mb-5 rounded-lg bg-gray-50 p-3 dark:bg-dark-900 lg:hidden"
          >
            <summary class="cursor-pointer text-sm font-medium">
              本页目录
            </summary>
            <nav class="mt-3 space-y-2">
              <a
                v-for="heading in rendered.toc"
                :key="heading.id"
                :href="`#${heading.id}`"
                class="block text-sm text-gray-500"
                :class="heading.level > 2 ? 'pl-3' : ''"
                >{{ heading.text }}</a
              >
            </nav>
          </details>
          <TutorialContent
            :html="tutorial.content_html"
            :markdown="tutorial.content_markdown"
            @retry="load"
          />
          <div class="mt-8 border-t border-gray-200 pt-5 dark:border-dark-700">
            <button class="btn btn-secondary" @click="supportOpen = true">
              仍未解决？联系售后
            </button>
          </div>
        </main>
        <aside v-if="rendered.toc.length" class="hidden lg:block">
          <nav
            class="sticky top-24 max-h-[75vh] space-y-2 overflow-y-auto border-l border-gray-200 pl-4 dark:border-dark-700"
            aria-label="本页目录"
          >
            <p class="mb-3 text-xs font-semibold text-gray-400">本页目录</p>
            <a
              v-for="heading in rendered.toc"
              :key="heading.id"
              :href="`#${heading.id}`"
              class="block text-sm text-gray-500 hover:text-primary-600"
              :class="heading.level > 2 ? 'pl-3 text-xs' : ''"
              >{{ heading.text }}</a
            >
          </nav>
        </aside>
      </div>
    </div>
    <TutorialSupportDialog :show="supportOpen" @close="supportOpen = false" />
  </component>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import AppLayout from "@/components/layout/AppLayout.vue";
import PublicLayout from "@/components/tutorial/TutorialPublicLayout.vue";
import TutorialContent from "@/components/tutorial/TutorialContent.vue";
import TutorialSupportDialog from "@/components/tutorial/TutorialSupportDialog.vue";
import {
  categoryLabel,
  errorMessage,
  renderTutorial,
} from "@/components/tutorial/content";
import { getTutorial, type Tutorial } from "@/api/tutorial";
import { useAuthStore } from "@/stores/auth";
const route = useRoute();
const auth = useAuthStore();
const tutorial = ref<Tutorial>();
const loading = ref(true);
const error = ref("");
const copied = ref(false);
const supportOpen = ref(false);
const rendered = computed(() =>
  renderTutorial(
    tutorial.value?.content_markdown || "",
    tutorial.value?.content_html || undefined,
  ),
);
let request = 0;
let copyTimer: ReturnType<typeof setTimeout> | undefined;
async function load() {
  const current = ++request;
  loading.value = true;
  error.value = "";
  try {
    const result = await getTutorial(
      String(route.params.slug),
      !auth.isAuthenticated,
    );
    if (current === request) {
      tutorial.value = result;
      document.title = `${result.title} - 教程与售后`;
    }
  } catch (e) {
    if (current === request)
      error.value = errorMessage(
        e,
        "该教程尚未发布、需要登录，或分享链接已经失效。",
      );
  } finally {
    if (current === request) loading.value = false;
  }
}
async function copyLink() {
  try {
    await navigator.clipboard.writeText(
      `${location.origin}/tutorial/${encodeURIComponent(String(route.params.slug))}`,
    );
    copied.value = true;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copied.value = false;
    }, 1800);
  } catch {
    error.value = "复制失败，请从浏览器地址栏复制链接。";
  }
}
watch(() => route.params.slug, load, { immediate: true });
onBeforeUnmount(() => {
  request++;
  clearTimeout(copyTimer);
});
</script>
