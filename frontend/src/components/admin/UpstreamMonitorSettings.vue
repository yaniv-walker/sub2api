<template>
  <section class="card" aria-labelledby="upstream-monitor-title">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="upstream-monitor-title" class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t('admin.settings.upstreamMonitor.title') }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.settings.upstreamMonitor.description') }}
      </p>
    </div>
    <div class="space-y-4 p-6" :aria-busy="loading || saving">
      <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <template v-else>
        <p v-if="loadFailed" role="alert" class="text-sm text-red-600">{{ t('admin.settings.upstreamMonitor.loadFailed') }}</p>
        <label for="upstream-monitor-enabled" class="flex items-center justify-between gap-4 text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.settings.upstreamMonitor.enabled') }}
          <Toggle id="upstream-monitor-enabled" v-model="enabled" :disabled="saving || loadFailed"
            :aria-label="t('admin.settings.upstreamMonitor.enabled')" data-testid="upstream-monitor-enabled" />
        </label>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.upstreamMonitor.hint') }}</p>
        <p v-if="!loadFailed" class="text-sm text-gray-600 dark:text-gray-300" data-testid="upstream-monitor-status" aria-live="polite">
          {{ t(savedEnabled ? 'admin.settings.upstreamMonitor.currentEnabled' : 'admin.settings.upstreamMonitor.currentDisabled') }}
        </p>
        <p v-if="message" role="status" class="text-sm" :class="saveFailed ? 'text-red-600' : 'text-green-600'">{{ message }}</p>
        <div class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700">
          <button v-if="loadFailed" type="button" class="btn btn-primary btn-sm" @click="load">{{ t('admin.settings.upstreamMonitor.retry') }}</button>
          <button v-else type="button" class="btn btn-primary btn-sm" :disabled="saving || enabled === savedEnabled" data-testid="upstream-monitor-save" @click="save">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { getUpstreamMonitorSettings, updateUpstreamMonitorSettings } from '@/api/admin/upstreamMonitorSettings'

const { t } = useI18n()
const enabled = ref(false)
const savedEnabled = ref(false)
const loading = ref(true)
const saving = ref(false)
const loadFailed = ref(false)
const saveFailed = ref(false)
const message = ref('')

async function load() {
  loading.value = true
  loadFailed.value = false
  message.value = ''
  try {
    const settings = await getUpstreamMonitorSettings()
    enabled.value = savedEnabled.value = settings.enabled
  } catch {
    loadFailed.value = true
  } finally {
    loading.value = false
  }
}

async function save() {
  if (saving.value || loadFailed.value) return
  saving.value = true
  saveFailed.value = false
  message.value = ''
  try {
    const settings = await updateUpstreamMonitorSettings(enabled.value)
    enabled.value = savedEnabled.value = settings.enabled
    message.value = t('admin.settings.upstreamMonitor.saved')
  } catch {
    saveFailed.value = true
    message.value = t('admin.settings.upstreamMonitor.saveFailed')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
