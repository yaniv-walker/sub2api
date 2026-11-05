<template>
  <Teleport to="body"
    ><div
      v-if="show"
      class="fixed inset-0 z-50 grid place-items-center overflow-y-auto bg-slate-900/60 p-4"
      @click.self="$emit('close')"
      @keydown.esc="$emit('close')"
    >
      <section
        ref="panel"
        class="max-h-[90vh] w-full max-w-2xl overflow-auto rounded-xl bg-white p-6 shadow-xl dark:bg-dark-800"
        role="dialog"
        aria-modal="true"
        aria-labelledby="tutorial-support-title"
        tabindex="-1"
        @keydown.tab="trapFocus"
      >
        <header class="flex items-start justify-between gap-4">
          <div>
            <h2
              id="tutorial-support-title"
              class="text-xl font-semibold text-gray-900 dark:text-white"
            >
              {{ support?.title || "联系售后" }}
            </h2>
            <p class="mt-2 text-sm text-gray-500">
              {{
                support?.description ||
                "提供注册邮箱、客户端名称与报错截图，有助于更快定位问题。"
              }}
            </p>
          </div>
          <button class="btn btn-secondary btn-sm shrink-0 whitespace-nowrap" @click="$emit('close')">
            关闭
          </button>
        </header>
        <p v-if="error" class="mt-4 text-sm text-red-600" role="alert">
          {{ error }}
        </p>
        <p v-else-if="loading" class="py-6 text-sm text-gray-500">
          正在加载联系方式…
        </p>
        <template v-else
          ><div class="mt-5 grid gap-4 sm:grid-cols-2">
            <article
              v-for="item in contacts"
              :key="item.label"
              class="rounded-lg border border-gray-200 p-4 dark:border-dark-700"
            >
              <h3 class="font-medium text-gray-900 dark:text-white">
                {{ item.label }}
              </h3>
              <p v-if="item.account" class="mt-2 select-all text-sm">
                {{ item.account }}
              </p>
              <img
                v-if="item.url"
                :src="item.url"
                :alt="`${item.label}二维码`"
                class="mt-3 aspect-square w-full rounded bg-white object-contain"
                @error="
                  ($event.target as HTMLImageElement).alt =
                    '二维码暂时无法显示，请使用文字联系方式'
                "
              />
            </article>
          </div>
          <p v-if="support?.email" class="mt-4 text-sm">
            邮箱：<a
              :href="`mailto:${support.email}`"
              class="text-primary-600 underline"
              >{{ support.email }}</a
            >
          </p>
          <a
            v-if="safeTicket"
            :href="safeTicket"
            target="_blank"
            rel="noopener noreferrer"
            class="btn btn-secondary mt-4"
            >提交工单 ↗</a
          >
          <p v-if="support?.service_hours" class="mt-4 text-xs text-gray-500">
            服务时间：{{ support.service_hours }}
          </p>
          <p
            v-if="!contacts.length && !support?.email && !safeTicket"
            class="mt-5 rounded-lg bg-gray-50 p-4 text-sm text-gray-500 dark:bg-dark-900"
          >
            暂未配置联系方式，请通过购买渠道联系管理员。
          </p></template
        >
      </section>
    </div></Teleport
  >
</template>
<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { getTutorialSupport, type TutorialSupport } from "@/api/tutorial";
import { errorMessage } from "./content";
import { useAuthStore } from "@/stores/auth";
const auth = useAuthStore();
const props = defineProps<{ show: boolean }>();
defineEmits<{ close: [] }>();
const support = ref<TutorialSupport>();
const error = ref("");
const loading = ref(false);
const panel = ref<HTMLElement>();
let previousFocus: HTMLElement | null = null;
const contacts = computed(() =>
  [
    {
      label: "微信",
      account: support.value?.wechat,
      url: support.value?.wechat_qr_url,
    },
    {
      label: "个人 QQ",
      account: support.value?.qq,
      url: support.value?.qq_qr_url,
    },
    { label: "QQ 群", account: support.value?.qq_group, url: "" },
  ].filter((x) => x.account || x.url),
);
const safeTicket = computed(() =>
  /^https:\/\//.test(support.value?.ticket_url || "")
    ? support.value?.ticket_url
    : "",
);
watch(
  () => props.show,
  async (show) => {
    if (!show) {
      previousFocus?.focus();
      return;
    }
    previousFocus = document.activeElement as HTMLElement;
    await nextTick();
    panel.value?.focus();
    loading.value = true;
    error.value = "";
    try {
      support.value = await getTutorialSupport(false, !auth.isAuthenticated);
    } catch (e) {
      error.value = errorMessage(e, "联系方式加载失败，请稍后重试。");
    } finally {
      loading.value = false;
    }
  },
  { immediate: true },
);
function trapFocus(event: KeyboardEvent) {
  const nodes = [
    ...(panel.value?.querySelectorAll<HTMLElement>(
      'button,a[href],input,select,textarea,[tabindex="0"]',
    ) || []),
  ];
  const first = nodes[0];
  const last = nodes[nodes.length - 1];
  if (
    event.shiftKey &&
    (document.activeElement === first || document.activeElement === panel.value)
  ) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}
</script>
