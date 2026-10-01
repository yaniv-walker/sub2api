<template>
  <section class="card" aria-labelledby="request-observability-title">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="request-observability-title" class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t('admin.settings.requestObservability.title') }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.settings.requestObservability.description') }}
      </p>
    </div>
    <div class="space-y-4 p-6" :aria-busy="loading || saving">
      <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <template v-else>
        <p v-if="loadFailed" role="alert" class="text-sm text-red-600">
          {{ t('admin.settings.requestObservability.loadFailed') }}
        </p>
        <label for="request-observability-enabled" class="flex items-center justify-between gap-4 text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.settings.requestObservability.enabled') }}
          <Toggle id="request-observability-enabled" v-model="enabled"
            :disabled="saving || loadFailed" aria-describedby="request-observability-hint"
            :aria-label="t('admin.settings.requestObservability.enabled')"
            data-testid="request-observability-enabled" />
        </label>
        <p id="request-observability-hint" class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.settings.requestObservability.hint') }}
        </p>
        <p v-if="!loadFailed" class="text-sm text-gray-600 dark:text-gray-300" data-testid="request-observability-status" aria-live="polite">
          {{ t(savedEnabled ? 'admin.settings.requestObservability.currentEnabled' : 'admin.settings.requestObservability.currentDisabled') }}
        </p>
        <p v-if="message" role="status" class="text-sm" :class="saveFailed ? 'text-red-600' : 'text-green-600'">
          {{ message }}
        </p>
        <div class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700">
          <button v-if="loadFailed" type="button" class="btn btn-primary btn-sm" @click="load">
            {{ t('admin.settings.requestObservability.retry') }}
          </button>
          <button v-else type="button" class="btn btn-primary btn-sm" :disabled="saving || enabled === savedEnabled"
            data-testid="request-observability-save" @click="save">
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
import { getRequestObservabilitySettings, updateRequestObservabilitySettings } from '@/api/admin/requestObservability'

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
    const settings = await getRequestObservabilitySettings()
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
  message.value = ''
  saveFailed.value = false
  try {
    const settings = await updateRequestObservabilitySettings(enabled.value)
    enabled.value = savedEnabled.value = settings.enabled
    message.value = t('admin.settings.requestObservability.saved')
  } catch {
    saveFailed.value = true
    message.value = t('admin.settings.requestObservability.saveFailed')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
