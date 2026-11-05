<template>
  <div
    class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-900 dark:text-gray-100"
  >
    <header
      class="border-b border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800"
    >
      <div
        class="mx-auto flex max-w-6xl items-center justify-between gap-3 px-4 py-4"
      >
        <RouterLink to="/tutorial" class="font-semibold"
          >{{ app.siteName || "教程中心" }} · 教程中心</RouterLink
        ><RouterLink to="/login" class="btn btn-primary btn-sm"
          >登录网站</RouterLink
        >
      </div>
    </header>
    <main class="mx-auto max-w-6xl px-4 py-8 sm:px-6"><slot /></main>
  </div>
</template>
<script setup lang="ts">
import { useAppStore } from "@/stores";
const app = useAppStore();
</script>
