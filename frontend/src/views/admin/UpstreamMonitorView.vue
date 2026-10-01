<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="flex flex-col gap-3 border-b border-gray-200 pb-5 dark:border-dark-700 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 class="text-lg font-semibold text-gray-900 dark:text-white">上游监控</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">按上游统一额度查看余额、关联账号和运行状态。</p>
        </div>
        <button class="btn btn-primary btn-sm" type="button" :disabled="refreshing || loading" @click="refreshAllData">
          <Icon name="refresh" size="sm" /> {{ refreshing ? '刷新中…' : '刷新全部' }}
        </button>
      </section>

      <div v-if="error" class="border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-300" role="alert">
        {{ error }} <button class="ml-2 underline" type="button" @click="load">重试</button>
      </div>
      <div v-if="refreshMessage" class="border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/20 dark:text-amber-200">{{ refreshMessage }}</div>

      <div v-if="loading" class="grid grid-cols-2 gap-4 lg:grid-cols-4" aria-busy="true">
        <div v-for="i in 4" :key="i" class="h-24 animate-pulse bg-gray-100 dark:bg-dark-800" />
      </div>
      <div v-else class="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <div v-for="item in metrics" :key="item.label" class="card border border-gray-200 p-4 dark:border-dark-700">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ item.label }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ item.value }}</p>
        </div>
      </div>

      <div class="grid gap-6 xl:grid-cols-2">
        <section class="card overflow-hidden border border-gray-200 dark:border-dark-700">
          <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700"><h2 class="font-medium text-gray-900 dark:text-white">上游</h2><span class="text-xs text-gray-500">{{ upstreams.length }} 个</span></div>
          <div v-if="!loading && upstreams.length === 0" class="px-4 py-10 text-center text-sm text-gray-500">暂无上游配置</div>
          <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
            <div v-for="upstream in upstreams" :key="upstream.id" class="flex items-center justify-between gap-3 px-4 py-3 hover:bg-gray-50 dark:hover:bg-dark-800">
              <button type="button" class="min-w-0 flex-1 text-left" @click="openUpstream(upstream)">
                <span class="block truncate font-medium text-gray-900 dark:text-white">{{ upstream.name || upstream.base_url }}</span><span class="block truncate text-xs text-gray-500">{{ upstream.base_url }} · {{ upstream.upstream_type || upstream.type || '未分类' }}</span><span v-if="upstream.credential_error" class="block truncate text-xs text-amber-600">需重新配置凭据</span>
              </button>
              <span class="whitespace-nowrap text-sm font-medium text-gray-900 dark:text-white">{{ money(upstream.balance) }}</span>
              <button type="button" class="btn btn-secondary btn-sm" @click="openConfig(upstream)">配置</button>
            </div>
          </div>
        </section>

        <section class="card overflow-hidden border border-gray-200 dark:border-dark-700">
          <div class="flex flex-col gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between"><h2 class="font-medium text-gray-900 dark:text-white">监控账号</h2><input v-model="query" class="input input-sm w-full sm:w-48" type="search" placeholder="搜索账号或域名" aria-label="搜索监控账号" /></div>
          <div v-if="!loading && filteredAccounts.length === 0" class="px-4 py-10 text-center text-sm text-gray-500">暂无匹配账号</div>
          <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
            <button v-for="account in filteredAccounts" :key="account.id" type="button" class="flex w-full items-center justify-between gap-3 px-4 py-3 text-left hover:bg-gray-50 dark:hover:bg-dark-800" @click="openAccount(account)">
              <span class="min-w-0"><span class="block truncate font-medium text-gray-900 dark:text-white">{{ account.name }}</span><span class="block truncate text-xs text-gray-500">{{ account.base_url }} · {{ account.type }}</span></span><span class="whitespace-nowrap text-sm font-medium text-gray-900 dark:text-white">{{ money(account.balance) }}</span>
            </button>
          </div>
        </section>
      </div>

      <div v-if="configuring" class="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-4" @click.self="configuring = null">
        <form class="w-full max-w-lg space-y-4 rounded-lg bg-white p-5 shadow-xl dark:bg-dark-900" @submit.prevent="saveConfig">
          <div class="flex items-start justify-between"><div><h2 class="text-lg font-semibold text-gray-900 dark:text-white">配置上游</h2><p class="mt-1 text-xs text-gray-500">{{ configuring.base_url }}</p></div><button type="button" class="text-gray-500" @click="configuring = null">×</button></div>
          <div class="grid gap-3 sm:grid-cols-2"><label class="text-sm">名称<input v-model="configForm.name" class="input mt-1 w-full" /></label><label class="text-sm">类型<select v-model="configForm.type" class="input mt-1 w-full"><option value="sub2api">sub2api</option><option value="nexapi">nexapi</option></select></label></div>
          <label class="flex items-center gap-2 text-sm"><input v-model="configForm.enabled" type="checkbox" /> 启用余额监控</label>
          <label class="text-sm">余额换算系数<input v-model.number="configForm.quota_divider" type="number" min="0.000001" step="0.000001" class="input mt-1 w-full" /></label>
          <p class="text-xs text-gray-500">凭据按上游单独保存；留空表示保持原值。已保存凭据不会回显。</p>
          <label class="text-sm">Personal Access Token<input v-model="configForm.personal_access_token" type="password" autocomplete="new-password" class="input mt-1 w-full" placeholder="仅在需要更新时填写" /></label>
          <label class="text-sm">Passkey<input v-model="configForm.passkey" type="password" autocomplete="new-password" class="input mt-1 w-full" placeholder="仅在需要更新时填写" /></label>
          <label class="text-sm">兼容旧 Access Token<input v-model="configForm.access_token" type="password" autocomplete="new-password" class="input mt-1 w-full" placeholder="仅在需要更新时填写" /></label>
          <div v-if="configError" class="text-sm text-red-600">{{ configError }}</div><div class="flex justify-end gap-2"><button type="button" class="btn btn-secondary" @click="configuring = null">取消</button><button type="submit" class="btn btn-primary" :disabled="configSaving">{{ configSaving ? '保存中…' : '保存配置' }}</button></div>
        </form>
      </div>

      <div v-if="selected" class="fixed inset-0 z-40 bg-black/30" aria-hidden="true" @click="selected = null" />
      <aside v-if="selected" class="fixed inset-y-0 right-0 z-50 w-full max-w-xl overflow-y-auto bg-white p-5 shadow-xl dark:bg-dark-900" role="dialog" aria-modal="true" :aria-label="detailTitle || '上游详情'">
        <div class="flex items-start justify-between gap-4"><div><h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ detailTitle }}</h2><p class="mt-1 text-xs text-gray-500">{{ selected.base_url }}</p></div><button class="btn btn-secondary btn-sm" type="button" aria-label="关闭详情" @click="selected = null">关闭</button></div>
        <div class="mt-5 grid grid-cols-2 gap-3"><div class="border border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500">余额</p><p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ money(selected.balance) }}</p></div><div class="border border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500">类型</p><p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ isUpstream(selected) ? (selected.upstream_type || selected.type || '未分类') : selected.type }}</p></div></div>
        <div class="mt-6 flex gap-2 border-b border-gray-200 dark:border-dark-700"><button v-for="tab in tabs" :key="tab.key" type="button" class="border-b-2 px-2 py-2 text-sm" :class="activeTab === tab.key ? 'border-primary-500 text-primary-600' : 'border-transparent text-gray-500'" @click="activeTab = tab.key">{{ tab.label }}</button></div>
        <div v-if="analyticsLoading" class="py-10 text-center text-sm text-gray-500">加载分析数据…</div>
        <div v-else-if="analyticsError" class="mt-5 border border-amber-200 bg-amber-50 px-4 py-4 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/20 dark:text-amber-200" role="alert">{{ analyticsError }} <button class="ml-2 underline" type="button" @click="loadAnalytics">重试</button></div>
        <div v-else-if="isUpstream(selected) && activeTab === 'errors' && analytics" class="mt-5 space-y-5 text-sm">
          <div class="grid grid-cols-2 gap-3"><div class="border border-gray-200 p-3 dark:border-dark-700"><p class="text-gray-500">错误总数</p><p class="mt-1 text-xl font-semibold">{{ analytics.total_errors }}</p></div><div class="border border-gray-200 p-3 dark:border-dark-700"><p class="text-gray-500">受影响账号</p><p class="mt-1 text-xl font-semibold">{{ analytics.affected_account_count }}</p></div></div>
          <p class="text-xs text-gray-500">错误率：{{ analytics.error_rate == null ? '请求总数未知，暂无法计算' : `${(analytics.error_rate * 100).toFixed(2)}%` }}</p>
          <section><h3 class="font-medium">错误类型</h3><p v-if="!analytics.by_type?.length" class="mt-2 text-gray-500">该时间范围暂无错误记录</p><div v-for="item in analytics.by_type" :key="item.type" class="mt-2 flex justify-between border-b border-gray-100 pb-2 dark:border-dark-700"><span>{{ item.type }}</span><span>{{ item.count }}</span></div></section>
          <section v-if="analytics.daily?.length"><h3 class="font-medium">每日错误</h3><div v-for="item in analytics.daily" :key="item.date" class="mt-2 flex items-center gap-3"><span class="w-24 shrink-0 text-xs text-gray-500">{{ item.date }}</span><div class="h-2 flex-1 bg-gray-100 dark:bg-dark-700"><div class="h-2 bg-red-400" :style="{ width: barWidth(item.count, maxErrors) }" /></div><span class="w-8 text-right">{{ item.count }}</span></div></section>
          <section v-if="analytics.recent?.length"><h3 class="font-medium">最近错误</h3><div v-for="(item, index) in analytics.recent" :key="`${item.account_id}-${item.occurred_at}-${index}`" class="border-b border-gray-100 py-2 dark:border-dark-700"><div class="flex justify-between gap-2"><span>{{ item.error_type }} · 账号 {{ item.account_id }}</span><span class="text-xs text-gray-500">{{ item.error_code || '—' }}</span></div><p class="mt-1 text-xs text-gray-500">{{ formatDate(item.occurred_at) }}</p></div></section>
        </div>
        <div v-else-if="isUpstream(selected) && activeTab === 'usage' && analytics" class="mt-5 space-y-5 text-sm">
          <div class="grid grid-cols-2 gap-3"><div class="border border-gray-200 p-3 dark:border-dark-700"><p class="text-gray-500">期初 / 最新余额</p><p class="mt-1 font-semibold">{{ money(analytics.starting_balance) }} / {{ money(analytics.ending_balance) }} {{ analytics.currency }}</p></div><div class="border border-gray-200 p-3 dark:border-dark-700"><p class="text-gray-500">累计消耗 / 日均</p><p class="mt-1 font-semibold">{{ money(analytics.total_cost) }} / {{ money(analytics.average_daily_cost) }}</p></div></div>
          <p class="text-xs text-gray-500">{{ analytics.total_requests == null ? '暂无请求事件数据' : `请求数：${analytics.total_requests}` }} · 余额快照 {{ analytics.data_quality?.snapshot_count ?? 0 }} 条</p>
          <section><h3 class="font-medium">每日消耗与余额</h3><p v-if="!analytics.daily?.length" class="mt-2 text-gray-500">至少需要两条余额快照才能生成趋势</p><div v-for="item in analytics.daily" :key="item.date" class="mt-2 space-y-1 border-b border-gray-100 pb-2 dark:border-dark-700"><div class="flex justify-between"><span>{{ item.date }}</span><span>消耗 {{ money(item.cost) }} · 余额 {{ money(item.balance) }}</span></div><div class="h-2 bg-gray-100 dark:bg-dark-700"><div class="h-2 bg-primary-500" :style="{ width: barWidth(item.cost, maxCost) }" /></div></div></section>
          <section v-if="analytics.anomalies?.length"><h3 class="font-medium">余额回升（不计为消耗）</h3><p v-for="(item, index) in analytics.anomalies" :key="`${item.date}-${index}`" class="mt-2 text-amber-700 dark:text-amber-300">{{ item.date }} · 增加 {{ money(item.amount) }}</p></section>
        </div>
        <div v-else-if="isUpstream(selected) && activeTab === 'prediction' && prediction" class="mt-5 space-y-4 text-sm"><div class="grid grid-cols-2 gap-3"><div class="border border-gray-200 p-3 dark:border-dark-700">当前余额<p class="mt-1 text-xl font-semibold">{{ money(prediction.current_balance) }}</p></div><div class="border border-gray-200 p-3 dark:border-dark-700">日均消耗<p class="mt-1 text-xl font-semibold">{{ money(prediction.daily_burn_rate) }}</p></div></div><p class="font-medium">{{ prediction.estimated_days_left == null ? '历史数据不足或暂无可计算的消耗' : `预计可用 ${Math.ceil(prediction.estimated_days_left)} 天` }}</p><p v-if="prediction.estimated_depletion_date">预计耗尽：{{ formatDate(prediction.estimated_depletion_date) }}</p><p class="text-xs text-gray-500">历史快照 {{ prediction.data_points }} 条 · 算法 {{ prediction.algorithm }} · 置信度 {{ prediction.confidence == null ? '暂无' : `${(prediction.confidence * 100).toFixed(0)}%` }}</p></div>
        <div v-else class="mt-5 space-y-3 text-sm"><p v-if="activeTab === 'errors'">错误数：{{ analytics?.total_errors ?? '暂无' }}</p><p v-else-if="activeTab === 'usage'">日均消耗：{{ money(analytics?.average_daily_cost) }}</p><p v-else>{{ predictionText }}</p></div>
      </aside>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { MonitorAccount, UpstreamConfigInput, UpstreamSummary } from '@/api/admin/upstreamMonitor'

const loading = ref(true); const refreshing = ref(false); const error = ref(''); const refreshMessage = ref(''); const query = ref('')
const overview = ref<any>(null); const upstreams = ref<UpstreamSummary[]>([]); const accounts = ref<MonitorAccount[]>([])
const selected = ref<(UpstreamSummary | MonitorAccount) | null>(null); const activeTab = ref('errors'); const analytics = ref<any>(null); const prediction = ref<any>(null); const analyticsLoading = ref(false); const analyticsError = ref(''); let analyticsRequest = 0
const configuring = ref<UpstreamSummary | null>(null); const configSaving = ref(false); const configError = ref('')
const configForm = ref<UpstreamConfigInput>({ base_url: '', name: '', type: 'sub2api', enabled: true, quota_divider: 1, access_token: '', personal_access_token: '', passkey: '' })
const tabs = [{ key: 'errors', label: '错误统计' }, { key: 'usage', label: '用量趋势' }, { key: 'prediction', label: '余额预测' }]
const filteredAccounts = computed(() => accounts.value.filter(a => `${a.name} ${a.base_url} ${a.type}`.toLowerCase().includes(query.value.toLowerCase())))
const metrics = computed(() => [{ label: '总余额', value: money(overview.value?.total_balance) }, { label: '上游数量', value: overview.value?.total_upstreams ?? upstreams.value.length }, { label: '关联账号', value: overview.value?.total_accounts ?? accounts.value.length }, { label: '24 小时错误', value: overview.value?.error_count_24h ?? 0 }])
const detailTitle = computed(() => selected.value && ('upstream_type' in selected.value ? (selected.value.name || '上游详情') : selected.value.name))
const predictionText = computed(() => prediction.value?.days_remaining == null ? '暂无足够历史快照' : `预计还可使用 ${prediction.value.days_remaining} 天`)
const maxErrors = computed(() => Math.max(1, ...(analytics.value?.daily || []).map((row: { count: number }) => row.count)))
const maxCost = computed(() => Math.max(0.001, ...(analytics.value?.daily || []).map((row: { cost: number }) => row.cost)))
function barWidth(value: number, maximum: number) { return `${Math.max(0, Math.min(100, (value / maximum) * 100))}%` }
function formatDate(value: string) { return new Date(value).toLocaleString() }
function money(value?: number | null) { return value == null || Number.isNaN(Number(value)) ? '暂无' : Number(value).toFixed(2) }
function isUpstream(value: UpstreamSummary | MonitorAccount): value is UpstreamSummary { return 'upstream_type' in value || 'enabled' in value }
function mergeUpstreamBalances(items: UpstreamSummary[], balances?: Array<UpstreamSummary & { account_id?: number }>) { if (!balances?.length) return items; const byUrl = new Map(balances.map(item => [item.base_url.replace(/\/$/, '').toLowerCase(), item.balance])); return items.map(item => { const balance = byUrl.get(item.base_url.replace(/\/$/, '').toLowerCase()); return balance === undefined ? item : { ...item, balance } }) }
async function load() { loading.value = true; error.value = ''; try { const [o, u, a] = await Promise.all([adminAPI.upstreamMonitor.getOverview(), adminAPI.upstreamMonitor.listUpstreams(), adminAPI.upstreamMonitor.listAccounts()]); overview.value = o; upstreams.value = mergeUpstreamBalances(u, o.by_upstream); accounts.value = a.accounts; if (selected.value && isUpstream(selected.value)) { const current = upstreams.value.find(item => item.base_url.replace(/\/$/, '').toLowerCase() === selected.value?.base_url.replace(/\/$/, '').toLowerCase()); if (current) selected.value = current } } catch (e: any) { error.value = e?.response?.data?.error || e?.message || '加载上游监控失败' } finally { loading.value = false } }
async function refreshAllData() { refreshing.value = true; refreshMessage.value = ''; try { const result = await adminAPI.upstreamMonitor.refreshAll(); upstreams.value = mergeUpstreamBalances(upstreams.value, result.by_upstream); if (selected.value && isUpstream(selected.value)) { const current = upstreams.value.find(item => item.base_url.replace(/\/$/, '').toLowerCase() === selected.value?.base_url.replace(/\/$/, '').toLowerCase()); if (current) selected.value = current } refreshMessage.value = result.failed_count ? `已刷新 ${result.refreshed_count} 个账号，${result.failed_count} 个失败` : `已刷新 ${result.refreshed_count} 个账号`; await load() } catch (e: any) { refreshMessage.value = e?.response?.data?.error || e?.message || '刷新失败' } finally { refreshing.value = false } }
async function openUpstream(item: UpstreamSummary) { selected.value = item; activeTab.value = 'errors' }
async function openAccount(item: MonitorAccount) { selected.value = item; activeTab.value = 'errors' }
function openConfig(item: UpstreamSummary) { const type = (item.upstream_type || item.type || 'sub2api') as 'sub2api' | 'nexapi'; configuring.value = item; configError.value = ''; configForm.value = { base_url: item.base_url, name: item.name || '', type, enabled: item.enabled !== false, quota_divider: item.quota_divider || (type === 'sub2api' ? 1 : 500000), access_token: '', personal_access_token: '', passkey: '' } }
async function saveConfig() { if (!configuring.value) return; configSaving.value = true; configError.value = ''; try { const input = { ...configForm.value }; if (!input.access_token) delete input.access_token; if (!input.personal_access_token) delete input.personal_access_token; if (!input.passkey) delete input.passkey; await adminAPI.upstreamMonitor.configureUpstream(input); configuring.value = null; await load() } catch (e: any) { configError.value = e?.response?.data?.error || e?.message || '保存失败' } finally { configSaving.value = false } }
async function loadAnalytics() { const request = ++analyticsRequest; if (!selected.value) return; analyticsLoading.value = true; analyticsError.value = ''; analytics.value = null; prediction.value = null; const id = selected.value.id; const upstream = isUpstream(selected.value); if (upstream && !id) { analyticsError.value = '请先配置该上游，才能查看历史分析'; analyticsLoading.value = false; return } try { let result; if (activeTab.value === 'errors') result = upstream ? await adminAPI.upstreamMonitor.getUpstreamErrors(id) : await adminAPI.upstreamMonitor.getAccountErrors(id); else if (activeTab.value === 'usage') result = upstream ? await adminAPI.upstreamMonitor.getUpstreamUsage(id) : await adminAPI.upstreamMonitor.getAccountUsage(id); else result = upstream ? await adminAPI.upstreamMonitor.getUpstreamPrediction(id) : await adminAPI.upstreamMonitor.getAccountPrediction(id); if (request === analyticsRequest) { if (activeTab.value === 'prediction') prediction.value = result; else analytics.value = result } } catch (e: any) { if (request === analyticsRequest) analyticsError.value = e?.response?.data?.error?.message || e?.response?.data?.error || e?.message || '分析数据加载失败' } finally { if (request === analyticsRequest) analyticsLoading.value = false } }
watch([selected, activeTab], () => { void loadAnalytics() }); onMounted(() => { void load() })
</script>
