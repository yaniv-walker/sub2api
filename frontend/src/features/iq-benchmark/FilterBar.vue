<template>
  <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
    <div class="flex flex-1 flex-wrap items-center gap-2">
      <!-- 模型 -->
      <select v-model="local.model" class="input h-9 min-w-[9rem] py-0 text-xs">
        <option value="">{{ t('iqBenchmark.filter.modelAll') }}</option>
        <option v-for="model in modelOptions" :key="model" :value="model">{{ model }}</option>
      </select>

      <!-- 题目 -->
      <select v-model="local.caseId" class="input h-9 min-w-[9rem] py-0 text-xs">
        <option :value="null">{{ t('iqBenchmark.filter.caseAll') }}</option>
        <option v-for="item in caseOptions" :key="item.id" :value="item.id">{{ item.name }}</option>
      </select>

      <!-- 状态 -->
      <select v-model="local.status" class="input h-9 min-w-[8rem] py-0 text-xs">
        <option value="">{{ t('iqBenchmark.filter.statusAll') }}</option>
        <option value="success">{{ t('iqBenchmark.status.success') }}</option>
        <option value="failed">{{ t('iqBenchmark.status.failed') }}</option>
        <option value="timeout">{{ t('iqBenchmark.status.timeout') }}</option>
        <option value="blocked">{{ t('iqBenchmark.status.blocked') }}</option>
      </select>

      <!-- 类型 -->
      <select v-model="local.kind" class="input h-9 min-w-[8rem] py-0 text-xs">
        <option value="">{{ t('iqBenchmark.filter.kindAll') }}</option>
        <option value="image_url">{{ t('iqBenchmark.kind.image_url') }}</option>
        <option value="svg">{{ t('iqBenchmark.kind.svg') }}</option>
        <option value="markdown">{{ t('iqBenchmark.kind.markdown') }}</option>
        <option value="text">{{ t('iqBenchmark.kind.text') }}</option>
      </select>

      <!-- 时间范围 -->
      <select v-model.number="local.sinceDays" class="input h-9 min-w-[7rem] py-0 text-xs">
        <option :value="0">{{ t('iqBenchmark.filter.sinceAll') }}</option>
        <option :value="1">{{ t('iqBenchmark.filter.sinceDays', { n: 1 }) }}</option>
        <option :value="7">{{ t('iqBenchmark.filter.sinceDays', { n: 7 }) }}</option>
        <option :value="30">{{ t('iqBenchmark.filter.sinceDays', { n: 30 }) }}</option>
        <option :value="90">{{ t('iqBenchmark.filter.sinceDays', { n: 90 }) }}</option>
      </select>
    </div>

    <div class="flex items-center gap-2">
      <button
        v-if="!isDefault"
        class="btn btn-ghost btn-sm"
        @click="reset"
      >
        {{ t('iqBenchmark.filter.reset') }}
      </button>
      <button
        class="btn btn-secondary btn-sm"
        :disabled="loading || cooldownSeconds > 0"
        :title="cooldownSeconds > 0 ? t('iqBenchmark.error.cooldown', { n: cooldownSeconds }) : t('iqBenchmark.filter.refresh')"
        @click="$emit('refresh')"
      >
        <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IQFilterState } from './filters'
import { defaultFilterState, isDefaultFilterState } from './filters'
import type { IQPublicCase } from './types'

const props = withDefaults(
  defineProps<{
    modelValue: IQFilterState
    modelOptions?: string[]
    caseOptions?: IQPublicCase[]
    loading?: boolean
    /** 限流冷却剩余秒数；>0 时禁用刷新。 */
    cooldownSeconds?: number
  }>(),
  { modelOptions: () => [], caseOptions: () => [], loading: false, cooldownSeconds: 0 },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: IQFilterState): void
  (e: 'refresh'): void
  (e: 'reset'): void
}>()

const { t } = useI18n()

/** 本地副本：避免直接改父组件状态，改动后再向上抛。 */
const local = reactive<IQFilterState>({ ...props.modelValue })

watch(
  () => props.modelValue,
  (next) => Object.assign(local, next),
)

// 任何筛选变化都同步给父组件；父组件负责防抖加载。
watch(
  local,
  () => emit('update:modelValue', { ...local }),
  { deep: true },
)

const isDefault = computed(() => isDefaultFilterState(local))

function reset(): void {
  Object.assign(local, defaultFilterState())
  emit('reset')
}
</script>
