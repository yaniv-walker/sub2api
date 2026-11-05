<template>
  <section class="card space-y-4">
    <header>
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        售后与分享设置
      </h2>
      <p class="mt-1 text-sm text-gray-500">
        只显示已填写的联系方式。二维码可以填写本站素材地址或 HTTPS 图片地址。
      </p>
    </header>
    <p v-if="error" class="text-sm text-red-600" role="alert">{{ error }}</p>
    <form v-if="form" class="grid gap-4 md:grid-cols-2" @submit.prevent="save">
      <label v-for="field in fields" :key="field.key" class="input-label"
        >{{ field.label
        }}<input
          v-model="form[field.key]"
          class="input mt-1"
          :type="field.key === 'email' ? 'email' : 'text'"
          :maxlength="field.key.includes('url') ? 2048 : 500"
      /></label>
      <label class="flex items-center gap-2 text-sm md:col-span-2"
        ><input
          v-model="form.public_enabled"
          type="checkbox"
        />允许公开分享（还需在单篇教程中开启）</label
      >
      <div class="flex items-center gap-3 md:col-span-2">
        <button class="btn btn-primary" :disabled="saving">
          {{ saving ? "保存中…" : "保存售后设置" }}</button
        ><span v-if="saved" class="text-sm text-emerald-600" role="status"
          >设置已保存</span
        >
      </div>
    </form>
    <p v-else-if="!error" class="text-sm text-gray-500">正在加载设置…</p>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  getTutorialSupport,
  updateTutorialSupport,
  type TutorialSupport,
} from "@/api/tutorial";
import { errorMessage } from "./content";
const form = ref<TutorialSupport>();
const saving = ref(false);
const saved = ref(false);
const error = ref("");
const fields: {
  key: Exclude<keyof TutorialSupport, "public_enabled">;
  label: string;
}[] = [
  { key: "title", label: "弹窗标题" },
  { key: "description", label: "说明" },
  { key: "wechat", label: "微信号" },
  { key: "wechat_qr_url", label: "微信二维码图片地址" },
  { key: "qq", label: "个人 QQ" },
  { key: "qq_group", label: "QQ 群" },
  { key: "qq_qr_url", label: "QQ 二维码图片地址" },
  { key: "email", label: "客服邮箱" },
  { key: "ticket_url", label: "工单链接（HTTPS）" },
  { key: "service_hours", label: "服务时间" },
];
onMounted(async () => {
  try {
    form.value = await getTutorialSupport(true);
  } catch (e) {
    error.value = errorMessage(e, "设置加载失败");
  }
});
async function save() {
  if (!form.value) return;
  saving.value = true;
  error.value = "";
  saved.value = false;
  try {
    await updateTutorialSupport(form.value);
    saved.value = true;
  } catch (e) {
    error.value = errorMessage(e, "保存失败");
  } finally {
    saving.value = false;
  }
}
</script>
