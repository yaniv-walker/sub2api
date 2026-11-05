<template>
  <AppLayout>
    <div class="space-y-5">
      <header class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">
            教程与售后管理
          </h1>
          <p class="mt-1 text-sm text-gray-500">
            先保存草稿，确认后发布。已发布内容在再次发布前保持不变。
          </p>
        </div>
        <div class="flex gap-2">
          <button
            class="btn btn-secondary"
            @click="tab = tab === 'support' ? 'documents' : 'support'"
          >
            {{ tab === "support" ? "返回教程" : "售后与分享设置" }}</button
          ><button class="btn btn-primary" @click="newTutorial">
            新建教程
          </button>
        </div>
      </header>
      <TutorialSupportSettings v-if="tab === 'support'" />
      <div
        v-else
        class="grid items-start gap-4 xl:grid-cols-[260px_minmax(0,1fr)]"
      >
        <aside class="card space-y-3 p-3">
          <input
            v-model="search"
            class="input"
            placeholder="搜索教程"
            aria-label="搜索管理教程"
          />
          <p v-if="listError" class="text-sm text-red-600">{{ listError }}</p>
          <button
            v-for="item in items"
            :key="item.id"
            class="block w-full rounded-lg border p-3 text-left"
            :class="
              selected?.id === item.id
                ? 'border-primary-400 bg-primary-50 dark:bg-primary-900/20'
                : 'border-transparent hover:bg-gray-50 dark:hover:bg-dark-700'
            "
            @click="select(item.id)"
          >
            <span class="block truncate text-sm font-medium">{{
              item.title
            }}</span
            ><span class="mt-1 block text-xs text-gray-500"
              >{{ statusLabel(item.status) }} · /{{ item.slug }}</span
            >
          </button>
          <p
            v-if="!items.length && !loading"
            class="py-4 text-center text-sm text-gray-500"
          >
            还没有匹配的教程
          </p>
          <div class="flex items-center justify-between text-xs text-gray-500">
            <button
              class="btn btn-secondary btn-sm"
              :disabled="page <= 1"
              @click="
                page--;
                loadList();
              "
            >
              上一页</button
            ><span>{{ page }} / {{ Math.max(pages, 1) }}</span
            ><button
              class="btn btn-secondary btn-sm"
              :disabled="page >= pages"
              @click="
                page++;
                loadList();
              "
            >
              下一页
            </button>
          </div>
        </aside>
        <main class="card min-w-0 space-y-5 p-4 sm:p-5">
          <p
            v-if="error"
            class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300"
            role="alert"
          >
            {{ error }}
          </p>
          <p v-if="loading" class="py-8 text-sm text-gray-500">正在加载教程…</p>
          <form v-else class="space-y-5" @submit.prevent="save()">
            <div
              class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-200 pb-4 dark:border-dark-700"
            >
              <div class="text-xs text-gray-500">
                {{
                  selected
                    ? `${statusLabel(selected.status)} · 版本 ${selected.version}`
                    : "新教程"
                }}<span v-if="dirty" class="ml-2 font-semibold text-amber-600"
                  >有未保存更改</span
                >
              </div>
              <div class="flex flex-wrap gap-2">
                <button
                  v-if="selected"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="
                    showHistory = !showHistory;
                    loadHistory();
                  "
                >
                  历史版本</button
                ><a
                  v-if="selected?.status === 'published'"
                  :href="`/tutorial/${selected.slug}`"
                  target="_blank"
                  class="btn btn-secondary btn-sm"
                  >查看已发布页面 ↗</a
                >
              </div>
            </div>
            <section
              v-if="showHistory && selected"
              class="space-y-2 rounded-lg bg-gray-50 p-4 dark:bg-dark-900"
              aria-label="历史版本"
            >
              <h2 class="text-sm font-semibold">历史版本</h2>
              <p class="text-xs text-gray-500">
                恢复会生成草稿，不立即改变已发布页面。
              </p>
              <div
                v-for="revision in revisions"
                :key="revision.id"
                class="flex items-center justify-between gap-3 text-sm"
              >
                <span
                  >版本 {{ revision.version }} ·
                  {{
                    new Date(revision.created_at).toLocaleString("zh-CN")
                  }}</span
                ><button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="saving"
                  @click="restore(revision.id)"
                >
                  恢复为草稿
                </button>
              </div>
              <p v-if="!revisions.length" class="text-xs text-gray-500">
                暂无历史版本
              </p>
            </section>
            <div class="grid gap-4 md:grid-cols-2">
              <label class="input-label"
                >标题<input
                  v-model.trim="form.title"
                  class="input mt-1"
                  maxlength="160"
                  required /></label
              ><label class="input-label"
                >分享路径<input
                  v-model.trim="form.slug"
                  class="input mt-1"
                  pattern="[a-z0-9]+(?:-[a-z0-9]+)*"
                  placeholder="例如：codex"
                  required /></label
              ><label class="input-label"
                >分类<select v-model="form.category" class="input mt-1">
                  <option
                    v-for="item in tutorialCategories"
                    :key="item.value"
                    :value="item.value"
                  >
                    {{ item.label }}
                  </option>
                </select></label
              ><label class="input-label"
                >排序（数值小的在前）<input
                  v-model.number="form.sort_order"
                  type="number"
                  class="input mt-1" /></label
              ><label class="input-label md:col-span-2"
                >摘要<textarea
                  v-model="form.summary"
                  class="input mt-1"
                  maxlength="400"
                  rows="2"
                ></textarea></label
              ><label class="flex items-center gap-2 text-sm md:col-span-2"
                ><input
                  v-model="form.is_public"
                  type="checkbox"
                />允许公开分享（需同时开启全局分享设置）</label
              >
            </div>
            <TutorialEditor
              :key="selected?.id || 'new'"
              v-model="form.content_markdown"
              :tutorial-id="selected?.id"
              :document-version="selected?.version"
            />
            <section
              class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700"
            >
              <div class="flex items-center justify-between">
                <h2 class="text-sm font-semibold">下载与参考资源</h2>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="
                    form.resources.push({
                      label: '',
                      url: '',
                      version: '',
                      platform: '',
                      sha256: '',
                    })
                  "
                >
                  添加资源
                </button>
              </div>
              <div
                v-for="(resource, index) in form.resources"
                :key="index"
                class="grid gap-2 rounded-lg bg-gray-50 p-3 dark:bg-dark-900 md:grid-cols-2"
              >
                <input
                  v-model="resource.label"
                  class="input"
                  aria-label="资源名称"
                  placeholder="名称"
                  required
                /><input
                  v-model="resource.url"
                  class="input"
                  aria-label="资源地址"
                  placeholder="https:// 或本站路径"
                  required
                /><input
                  v-model="resource.platform"
                  class="input"
                  aria-label="资源平台"
                  placeholder="Windows / macOS / Linux"
                /><input
                  v-model="resource.version"
                  class="input"
                  aria-label="资源版本"
                  placeholder="版本"
                /><input
                  v-model="resource.sha256"
                  class="input md:col-span-2"
                  aria-label="资源 SHA256"
                  placeholder="SHA-256（可选）"
                /><button
                  type="button"
                  class="text-left text-xs text-red-600"
                  @click="form.resources.splice(index, 1)"
                >
                  移除资源卡片
                </button>
              </div>
            </section>
            <footer
              class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-200 pt-4 dark:border-dark-700"
            >
              <button
                v-if="selected"
                type="button"
                class="btn btn-secondary text-red-600"
                :disabled="saving"
                @click="remove"
              >
                删除教程
              </button>
              <div class="ml-auto flex flex-wrap gap-2">
                <button
                  v-if="selected?.status === 'published'"
                  type="button"
                  class="btn btn-secondary"
                  :disabled="saving"
                  @click="changeStatus('offline')"
                >
                  下线</button
                ><button class="btn btn-secondary" :disabled="saving">
                  {{ saving ? "处理中…" : "保存草稿" }}</button
                ><button
                  type="button"
                  class="btn btn-primary"
                  :disabled="saving"
                  @click="publish"
                >
                  保存并发布
                </button>
              </div>
            </footer>
          </form>
        </main>
      </div>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import AppLayout from "@/components/layout/AppLayout.vue";
import TutorialEditor from "@/components/tutorial/TutorialEditor.vue";
import TutorialSupportSettings from "@/components/tutorial/TutorialSupportSettings.vue";
import {
  errorMessage,
  htmlToMarkdown,
  statusLabel,
  tutorialCategories,
} from "@/components/tutorial/content";
import {
  createTutorial,
  deleteTutorial,
  getAdminTutorial,
  listAdminTutorials,
  listTutorialRevisions,
  restoreTutorialRevision,
  setTutorialStatus,
  updateTutorial,
  type Tutorial,
  type TutorialInput,
  type TutorialRevision,
} from "@/api/tutorial";
import { useAppStore } from "@/stores";
const route = useRoute();
const router = useRouter();
const app = useAppStore();
const items = ref<Tutorial[]>([]);
const selected = ref<Tutorial>();
const search = ref("");
const page = ref(1);
const pages = ref(1);
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const listError = ref("");
const tab = ref("documents");
const showHistory = ref(false);
const revisions = ref<TutorialRevision[]>([]);
const blank = (): TutorialInput => ({
  slug: "",
  title: "",
  summary: "",
  category: "getting-started",
  content_markdown: "# 新教程\n\n在这里填写教程正文。",
  is_public: false,
  sort_order: 100,
  resources: [],
});
const form = reactive<TutorialInput>(blank());
const baseline = ref(JSON.stringify(form));
const dirty = computed(() => JSON.stringify(form) !== baseline.value);
function fill(tutorial: Tutorial) {
  selected.value = tutorial;
  Object.assign(form, {
    slug: tutorial.slug,
    title: tutorial.title,
    summary: tutorial.summary,
    category: tutorial.category,
    content_markdown:
      tutorial.content_markdown || htmlToMarkdown(tutorial.content_html || ""),
    is_public: tutorial.is_public,
    sort_order: tutorial.sort_order,
    resources: structuredClone(tutorial.resources || []),
    version: tutorial.version,
  });
  baseline.value = JSON.stringify(form);
  error.value = "";
}
function discardAllowed() {
  return !dirty.value || confirm("当前更改尚未保存，确认离开编辑内容？");
}
async function loadList() {
  listError.value = "";
  try {
    const result = await listAdminTutorials({
      query: search.value,
      page: page.value,
      page_size: 20,
    });
    items.value = result.items;
    pages.value = result.pages;
  } catch (e) {
    listError.value = errorMessage(e, "列表加载失败");
  }
}
async function select(id: number, force = false) {
  if (!force && !discardAllowed()) return;
  tab.value = "documents";
  loading.value = true;
  error.value = "";
  try {
    const result = await getAdminTutorial(id);
    fill(result.tutorial);
    showHistory.value = false;
    await router.replace({ query: { ...route.query, id: String(id) } });
  } catch (e) {
    error.value = errorMessage(e, "教程加载失败");
  } finally {
    loading.value = false;
  }
}
function newTutorial() {
  if (!discardAllowed()) return;
  tab.value = "documents";
  selected.value = undefined;
  Object.keys(form).forEach(
    (key) => delete (form as unknown as Record<string, unknown>)[key],
  );
  Object.assign(form, blank());
  baseline.value = JSON.stringify(form);
  showHistory.value = false;
  void router.replace({ query: {} });
}
async function save(): Promise<Tutorial | undefined> {
  if (saving.value) return;
  if (
    !form.title.trim() ||
    !/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(form.slug) ||
    !form.content_markdown.trim()
  ) {
    error.value = "请填写标题、有效分享路径和正文。";
    return;
  }
  saving.value = true;
  error.value = "";
  try {
    const result = selected.value
      ? await updateTutorial(selected.value.id, { ...form })
      : await createTutorial({ ...form });
    fill(result);
    await router.replace({ query: { ...route.query, id: String(result.id) } });
    await loadList();
    app.showSuccess("草稿已保存");
    return result;
  } catch (e) {
    error.value = errorMessage(e, "保存失败，内容仍保留在编辑器中。");
    return undefined;
  } finally {
    saving.value = false;
  }
}
async function publish() {
  const saved = await save();
  if (!saved) return;
  await changeStatus("published");
}
async function changeStatus(status: Tutorial["status"]) {
  if (!selected.value) return;
  saving.value = true;
  error.value = "";
  try {
    const result = await setTutorialStatus(
      selected.value.id,
      status,
      selected.value.version,
    );
    fill(result);
    await loadList();
    app.showSuccess(status === "published" ? "教程已发布" : "教程已下线");
  } catch (e) {
    error.value = errorMessage(e, "状态修改失败");
  } finally {
    saving.value = false;
  }
}
async function remove() {
  if (
    !selected.value ||
    !confirm("确认删除这篇教程？分享链接将失效，仍被引用的素材会保留。")
  )
    return;
  saving.value = true;
  try {
    await deleteTutorial(selected.value.id, selected.value.version);
    baseline.value = JSON.stringify(form);
    newTutorial();
    await loadList();
  } catch (e) {
    error.value = errorMessage(e, "删除失败");
  } finally {
    saving.value = false;
  }
}
async function loadHistory() {
  if (!selected.value || !showHistory.value) return;
  try {
    revisions.value = await listTutorialRevisions(selected.value.id);
  } catch (e) {
    error.value = errorMessage(e, "版本加载失败");
  }
}
async function restore(revisionId: number) {
  if (!selected.value || !discardAllowed()) return;
  saving.value = true;
  try {
    fill(
      await restoreTutorialRevision(
        selected.value.id,
        revisionId,
        selected.value.version,
      ),
    );
    await loadList();
    app.showSuccess("历史版本已恢复为草稿，请检查后发布");
  } catch (e) {
    error.value = errorMessage(e, "恢复失败");
  } finally {
    saving.value = false;
  }
}
let timer: ReturnType<typeof setTimeout> | undefined;
watch(search, () => {
  clearTimeout(timer);
  timer = setTimeout(() => {
    page.value = 1;
    void loadList();
  }, 300);
});
function beforeUnload(event: BeforeUnloadEvent) {
  if (dirty.value) {
    event.preventDefault();
    event.returnValue = "";
  }
}
onBeforeRouteLeave(() => discardAllowed());
onMounted(async () => {
  window.addEventListener("beforeunload", beforeUnload);
  await loadList();
  const id = Number(route.query.id);
  if (id) await select(id, true);
  else if (items.value[0]) await select(items.value[0].id, true);
});
onBeforeUnmount(() => {
  window.removeEventListener("beforeunload", beforeUnload);
  clearTimeout(timer);
});
</script>
