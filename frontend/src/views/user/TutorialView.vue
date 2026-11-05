<template>
  <component :is="auth.isAuthenticated ? AppLayout : PublicLayout">
    <div class="space-y-6">
      <header
        class="flex flex-col justify-between gap-4 md:flex-row md:items-end"
      >
        <div>
          <p class="text-xs font-medium text-primary-600">教程与售后</p>
          <h1 class="mt-2 text-3xl font-semibold text-gray-900 dark:text-white">
            从第一次配置，到问题排查
          </h1>
          <p class="mt-3 text-sm text-gray-500">
            按使用场景查找教程，跟着步骤完成配置。
          </p>
        </div>
        <div class="flex gap-2">
          <RouterLink
            v-if="auth.isAdmin"
            to="/admin/tutorials"
            class="btn btn-secondary"
            >管理教程</RouterLink
          ><button class="btn btn-primary" @click="supportOpen = true">
            联系售后
          </button>
        </div>
      </header>
      <div class="flex flex-col gap-3 lg:flex-row">
        <input
          v-model="query"
          class="input flex-1"
          placeholder="搜索客户端、配置步骤或错误"
          aria-label="搜索教程"
        /><select
          v-model="category"
          class="input lg:!w-44"
          aria-label="教程分类"
        >
          <option value="">全部分类</option>
          <option
            v-for="item in tutorialCategories"
            :key="item.value"
            :value="item.value"
          >
            {{ item.label }}
          </option>
        </select>
      </div>
      <p class="text-xs text-gray-500" aria-live="polite">{{ total }} 篇教程</p>
      <div v-if="loading" class="grid gap-3 md:grid-cols-2" aria-busy="true">
        <div
          v-for="n in 4"
          :key="n"
          class="h-48 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800"
        ></div>
      </div>
      <section v-else-if="error" class="card p-8 text-center">
        <p class="text-sm text-red-600" role="alert">{{ error }}</p>
        <button class="btn btn-secondary mt-4" @click="load">重新加载</button>
      </section>
      <div v-else-if="items.length" class="grid gap-3 md:grid-cols-2">
        <RouterLink
          v-for="item in items"
          :key="item.id"
          :to="`/tutorial/${item.slug}`"
          class="card group block text-left"
          ><div class="flex justify-between">
            <span
              class="rounded-lg bg-primary-50 px-3 py-2 text-primary-600 dark:bg-primary-900/20"
              >▤</span
            ><span class="text-gray-400 transition group-hover:translate-x-1"
              >→</span
            >
          </div>
          <p class="mt-5 text-xs font-medium text-primary-600">
            {{ categoryLabel(item.category) }}
          </p>
          <h2 class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
            {{ item.title }}
          </h2>
          <p class="mt-2 line-clamp-3 text-sm leading-6 text-gray-500">
            {{ item.summary }}
          </p>
          <p
            class="mt-5 border-t border-gray-100 pt-3 text-xs text-gray-400 dark:border-dark-700"
          >
            更新于 {{ new Date(item.updated_at).toLocaleDateString("zh-CN") }}
          </p></RouterLink
        >
      </div>
      <p v-else class="card py-12 text-center text-sm text-gray-500">
        没有匹配的教程，试试其他关键词。
      </p>
      <nav
        v-if="pages > 1"
        class="flex items-center justify-center gap-4"
        aria-label="教程分页"
      >
        <button
          class="btn btn-secondary"
          :disabled="page <= 1 || loading"
          @click="
            page--;
            load();
          "
        >
          上一页</button
        ><span class="text-sm text-gray-500">{{ page }} / {{ pages }}</span
        ><button
          class="btn btn-secondary"
          :disabled="page >= pages || loading"
          @click="
            page++;
            load();
          "
        >
          下一页
        </button>
      </nav>
    </div>
    <TutorialSupportDialog :show="supportOpen" @close="supportOpen = false" />
  </component>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import AppLayout from "@/components/layout/AppLayout.vue";
import PublicLayout from "@/components/tutorial/TutorialPublicLayout.vue";
import TutorialSupportDialog from "@/components/tutorial/TutorialSupportDialog.vue";
import {
  categoryLabel,
  errorMessage,
  tutorialCategories,
} from "@/components/tutorial/content";
import { listTutorials, type Tutorial } from "@/api/tutorial";
import { useAuthStore } from "@/stores/auth";
const auth = useAuthStore();
const route = useRoute();
const query = ref("");
const category = ref("");
const page = ref(1);
const total = ref(0);
const pages = ref(1);
const items = ref<Tutorial[]>([]);
const loading = ref(true);
const error = ref("");
const supportOpen = ref(route.query.support === "1");
let request = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
async function load() {
  const current = ++request;
  loading.value = true;
  error.value = "";
  try {
    const result = await listTutorials(
      {
        query: query.value,
        category: category.value,
        page: page.value,
        page_size: 12,
      },
      !auth.isAuthenticated,
    );
    if (current === request) {
      items.value = result.items;
      total.value = result.total;
      pages.value = result.pages;
    }
  } catch (e) {
    if (current === request)
      error.value = errorMessage(e, "教程暂时无法加载，请稍后重试。");
  } finally {
    if (current === request) loading.value = false;
  }
}
watch([query, category], () => {
  clearTimeout(timer);
  timer = setTimeout(() => {
    page.value = 1;
    void load();
  }, 250);
});
watch(
  () => route.query.support,
  (value) => {
    if (value === "1") supportOpen.value = true;
  },
);
onMounted(load);
onBeforeUnmount(() => {
  clearTimeout(timer);
  request++;
});
</script>
