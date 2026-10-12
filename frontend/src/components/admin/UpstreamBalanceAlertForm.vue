<template>
  <section class="space-y-4" :aria-busy="loading || saving">
    <p v-if="loading" class="text-sm text-gray-500">{{ text('加载预警配置…', 'Loading alert settings…') }}</p>
    <template v-else>
      <div v-if="loadFailed" class="space-y-2">
        <p role="alert" class="text-sm text-red-600">{{ text('加载失败，请重试。', 'Unable to load settings. Please retry.') }}</p>
        <button type="button" class="btn btn-secondary btn-sm" @click="load">{{ text('重试', 'Retry') }}</button>
      </div>
      <fieldset :disabled="loadFailed || saving" class="space-y-4">
        <label for="upstream-balance-alert-enabled" class="flex items-center justify-between gap-4 text-sm font-medium">
          <span>{{ text('启用余额预警邮件', 'Enable balance alert emails') }}<span class="mt-1 block text-xs font-normal text-gray-500">{{ text('仅对当前上游生效，使用现有邮件服务。', 'Uses the existing email service for this upstream.') }}</span></span>
          <Toggle id="upstream-balance-alert-enabled" v-model="form.enabled" :disabled="loadFailed || saving" :aria-label="text('启用余额预警邮件', 'Enable balance alert emails')" />
        </label>
        <label class="block text-sm">{{ text('余额预警线', 'Balance threshold') }}
          <input v-model="form.threshold" data-testid="balance-alert-threshold" class="input mt-1" inputmode="decimal" :disabled="!form.enabled" />
          <span class="mt-1 block text-xs text-gray-500">{{ text('余额 ≤ 预警线时通知，单位与监控显示一致。', 'Notify when balance ≤ threshold, using the displayed balance unit.') }}<template v-if="balance != null"> {{ text('当前余额：', 'Current balance: ') }}{{ balance.toFixed(2) }}</template></span>
        </label>
        <label class="block text-sm">{{ text('收件邮箱', 'Recipients') }}
          <textarea v-model="recipients" data-testid="balance-alert-recipients" class="input mt-1" rows="2" :disabled="!form.enabled" placeholder="ops@example.com" />
          <span class="mt-1 block text-xs text-gray-500">{{ text('每行一个或用逗号分隔，最多 10 个。', 'One per line or separated by commas, up to 10.') }}</span>
        </label>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="text-sm">{{ text('提醒频率', 'Reminder frequency') }}<select v-model.number="form.repeat_interval_seconds" class="input mt-1" :disabled="!form.enabled"><option :value="0">{{ text('每次触线仅通知一次', 'Once per low balance episode') }}</option><option :value="86400">{{ text('持续低余额每 24 小时提醒', 'Every 24 hours while low') }}</option></select></label>
          <label class="text-sm">{{ text('邮件语言', 'Email language') }}<select v-model="form.locale" class="input mt-1" :disabled="!form.enabled"><option value="zh">中文</option><option value="en">English</option></select></label>
        </div>
      </fieldset>
      <div class="rounded-lg bg-gray-50 p-3 text-xs text-gray-500 dark:bg-dark-800">
        <div class="flex flex-wrap items-center justify-between gap-2"><span>{{ text('共享模板：上游余额预警', 'Shared template: upstream balance alert') }}</span><a href="/admin/settings?tab=email&amp;email_event=upstream.balance_low" target="_blank" rel="noopener noreferrer" class="text-primary-600 underline">{{ text('编辑共享模板', 'Edit shared template') }}</a></div>
        <p class="mt-2">{{ text('每 5 分钟检查一次。充值恢复后再次触线，可重新通知。', 'Checked every 5 minutes. After recovery, a new low balance episode can notify again.') }}</p>
      </div>
      <div v-if="rule" class="border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700" aria-live="polite">
        <p>{{ text('最近检查：', 'Last checked: ') }}{{ rule.state.last_checked_at ? new Date(rule.state.last_checked_at).toLocaleString() : text('尚未检查', 'Not checked yet') }}</p>
        <p class="mt-1">{{ text('最近通知：', 'Last notification: ') }}{{ deliverySummary }}</p>
        <p v-if="rule.state.last_error" class="mt-1 text-amber-700 dark:text-amber-300">{{ rule.state.last_error === 'balance_unavailable' ? text('无法获取有效余额，下次检查将重试。', 'Valid balance unavailable; retrying on the next check.') : text('邮件发送失败，将重试失败的收件人。', 'Email delivery failed; failed recipients will be retried.') }}</p>
      </div>
    </template>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="message" role="status" class="text-sm text-green-600">{{ message }}</p>
    <div class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"><button type="button" data-testid="balance-alert-save" class="btn btn-primary" :disabled="loading || saving || loadFailed || !dirty" @click="save">{{ saving ? text('保存中…', 'Saving…') : text('保存预警配置', 'Save alert settings') }}</button></div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { getBalanceAlert, saveBalanceAlert, type BalanceAlertConfig, type BalanceAlertRule } from '@/api/admin/upstreamMonitor'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ upstreamId: number; balance?: number | null }>()
const emit = defineEmits<{ dirty: [value: boolean]; saving: [value: boolean] }>()
const { locale } = useI18n()
const text = (zh: string, en: string) => locale.value.startsWith('zh') ? zh : en
const form = ref<BalanceAlertConfig>({ enabled: false, threshold: '10', recipients: [], repeat_interval_seconds: 0, locale: 'zh' })
const recipients = ref('')
const rule = ref<BalanceAlertRule | null>(null)
const loading = ref(true)
const saving = ref(false)
const loadFailed = ref(false)
const error = ref('')
const message = ref('')
const saved = ref('')
const snapshot = computed(() => JSON.stringify({ ...form.value, recipients: recipients.value }))
const dirty = computed(() => !loading.value && !loadFailed.value && snapshot.value !== saved.value)
watch(dirty, value => emit('dirty', value))
watch(saving, value => emit('saving', value))
const deliverySummary = computed(() => {
  const state = rule.value?.state
  const deliveries = Object.values(state?.deliveries || {})
  const times = deliveries.flatMap(d => d.sent_at ? [new Date(d.sent_at).getTime()] : []).filter(Number.isFinite)
  const failures = deliveries.filter(d => d.failures > 0).length
  if (!times.length) return failures ? text('发送失败', 'Delivery failed') : text('尚未发送', 'Not sent yet')
  return `${new Date(Math.max(...times)).toLocaleString()}${failures ? text(' · 部分失败', ' · Partially failed') : ''}`
})

function apply(value: BalanceAlertRule) {
  rule.value = value
  form.value = { enabled: value.enabled, threshold: value.threshold, recipients: [...value.recipients], repeat_interval_seconds: value.repeat_interval_seconds, locale: value.locale }
  recipients.value = value.recipients.join('\n')
  saved.value = snapshot.value
}
async function load() {
  loading.value = true
  loadFailed.value = false
  error.value = ''
  try { apply(await getBalanceAlert(props.upstreamId)) } catch { loadFailed.value = true } finally { loading.value = false }
}
async function save() {
  if (loading.value || loadFailed.value || saving.value) return
  error.value = ''; message.value = ''
  const emails = Array.from(new Map(recipients.value.split(/[,，\n]/).map(s => s.trim()).filter(Boolean).map(s => [s.toLowerCase(), s])).values())
  if (!/^(?:0|[1-9]\d{0,13})(?:\.\d{1,6})?$/.test(form.value.threshold.trim())) { error.value = text('请输入有效的非负预警线，最多 6 位小数。', 'Enter a non-negative threshold with up to 6 decimal places.'); return }
  if (emails.length > 10 || emails.some(e => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(e)) || (form.value.enabled && !emails.length)) { error.value = text('请填写有效邮箱，启用时至少 1 个、最多 10 个。', 'Enter valid recipients; 1–10 are required when enabled.'); return }
  saving.value = true
  try { apply(await saveBalanceAlert(props.upstreamId, { ...form.value, threshold: form.value.threshold.trim(), recipients: emails })); message.value = text('预警配置已保存。', 'Alert settings saved.') } catch (err) { error.value = extractApiErrorMessage(err, text('保存失败，请重试。', 'Unable to save. Please retry.')) } finally { saving.value = false }
}
onMounted(load)
</script>
