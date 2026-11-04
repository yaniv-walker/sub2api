<script setup lang="ts">
/**
 * 检测历史可见性管理。
 *
 * 历史墙默认对所有登录用户公开。遇到两类情况需要收起来：
 *   1. 某个分组不该出现在公开墙（内部测试组、客户专用组）
 *   2. 某个上游域名整体不该公开（私有部署、公司内网域名）
 *
 * 这里只做「隐藏」，不做删除 —— 隐藏是可逆的，删除不是，
 * 而管理动作应该先可逆。
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { fetchVisibility, setGroupHidden, setHostHidden } from '@/features/model-degradation/api'
import type { MDVisibility } from '@/features/model-degradation/types'
import '@/features/model-degradation/theme.css'

const { t } = useI18n()

const loading = ref(true)
const loadError = ref(false)
const savingKey = ref('')

/** 可见性配置。 */
const visibility = ref<MDVisibility>({ hidden_group_ids: [], hidden_hosts: [], hosts: [] })

/** 分组列表（当前只有已隐藏的，因为后端返回的就是隐藏集合 + 主机用量）。 */
const groups = ref<{ id: number; name: string; hidden: boolean }[]>([])

onMounted(async () => {
  await load()
})

async function load() {
  loading.value = true
  loadError.value = false
  try {
    const data = await fetchVisibility()
    visibility.value = data
    groups.value = data.hidden_group_ids.map((id) => ({
      id,
      name: `#${id}`,
      hidden: true
    }))
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
}

/** 主机行：合并「用量列表」与「已隐藏列表」。 */
const hostRows = computed(() => {
  const hidden = new Set(visibility.value.hidden_hosts)
  const rows = visibility.value.hosts.map((h) => ({
    host: h.host,
    runCount: h.run_count,
    hidden: hidden.has(h.host)
  }))

  // 已隐藏但当前没有用量的主机也要列出来，否则取消隐藏无从下手。
  for (const host of hidden) {
    if (!rows.some((r) => r.host === host)) {
      rows.push({ host, runCount: 0, hidden: true })
    }
  }

  return rows.sort((a, b) => b.runCount - a.runCount || a.host.localeCompare(b.host))
})

/** 已隐藏的主机数量。 */
const hiddenHostCount = computed(() => visibility.value.hidden_hosts.length)

/** 切换分组。 */
async function toggleGroup(id: number, hidden: boolean) {
  const key = `group-${id}`
  if (savingKey.value) return
  savingKey.value = key
  try {
    await setGroupHidden(id, hidden)
    const set = new Set(visibility.value.hidden_group_ids)
    if (hidden) set.add(id)
    else set.delete(id)
    visibility.value.hidden_group_ids = Array.from(set)
    groups.value = visibility.value.hidden_group_ids.map((gid) => ({
      id: gid,
      name: `#${gid}`,
      hidden: true
    }))
  } catch {
    loadError.value = true
  } finally {
    savingKey.value = ''
  }
}

/** 切换主机。 */
async function toggleHost(host: string, hidden: boolean) {
  const key = `host-${host}`
  if (savingKey.value) return
  savingKey.value = key
  try {
    await setHostHidden(host, hidden)
    const set = new Set(visibility.value.hidden_hosts)
    if (hidden) set.add(host)
    else set.delete(host)
    visibility.value.hidden_hosts = Array.from(set)
  } catch {
    loadError.value = true
  } finally {
    savingKey.value = ''
  }
}
</script>

<template>
  <AppLayout>
    <div class="ma-page">
      <div class="ma-page-inner">
        <header class="ma-top">
          <div class="ma-top-copy">
            <h1 class="ma-top-title">{{ t('modelDegradation.admin.title') }}</h1>
            <p class="ma-top-sub">{{ t('modelDegradation.admin.subtitle') }}</p>
          </div>
        </header>

        <p v-if="loadError" class="ma-banner is-error">{{ t('modelDegradation.errors.loadFailed') }}</p>
        <p v-if="loading" class="ma-banner">{{ t('modelDegradation.admin.loading') }}</p>

        <template v-else>
          <!-- 主机 -->
          <section class="ma-admin-card">
            <header class="ma-admin-head">
              <h2 class="ma-admin-title">{{ t('modelDegradation.admin.hostsTitle') }}</h2>
              <span class="ma-admin-sub">
                {{ t('modelDegradation.admin.hiddenCount', { n: hiddenHostCount }) }}
              </span>
            </header>
            <p class="ma-admin-hint">{{ t('modelDegradation.admin.hostsHint') }}</p>

            <div v-if="!hostRows.length" class="ma-admin-empty">
              {{ t('modelDegradation.admin.noHosts') }}
            </div>
            <ul v-else class="ma-admin-list">
              <li v-for="row in hostRows" :key="row.host" class="ma-admin-row">
                <div class="ma-admin-row-main">
                  <span class="ma-admin-host">{{ row.host }}</span>
                  <span class="ma-admin-count">
                    {{ t('modelDegradation.admin.runCount', { n: row.runCount }) }}
                  </span>
                </div>
                <button
                  type="button"
                  class="ma-toggle"
                  :class="{ 'is-on': row.hidden }"
                  :disabled="savingKey === `host-${row.host}`"
                  @click="toggleHost(row.host, !row.hidden)"
                >
                  {{ row.hidden ? t('modelDegradation.admin.hidden') : t('modelDegradation.admin.visible') }}
                </button>
              </li>
            </ul>
          </section>

          <!-- 分组 -->
          <section class="ma-admin-card">
            <header class="ma-admin-head">
              <h2 class="ma-admin-title">{{ t('modelDegradation.admin.groupsTitle') }}</h2>
              <span class="ma-admin-sub">{{ groups.length }}</span>
            </header>
            <p class="ma-admin-hint">{{ t('modelDegradation.admin.groupsHint') }}</p>

            <div v-if="!groups.length" class="ma-admin-empty">
              {{ t('modelDegradation.admin.noGroups') }}
            </div>
            <ul v-else class="ma-admin-list">
              <li v-for="row in groups" :key="row.id" class="ma-admin-row">
                <div class="ma-admin-row-main">
                  <span class="ma-admin-host">{{ row.name }}</span>
                </div>
                <button
                  type="button"
                  class="ma-toggle"
                  :class="{ 'is-on': row.hidden }"
                  :disabled="savingKey === `group-${row.id}`"
                  @click="toggleGroup(row.id, !row.hidden)"
                >
                  {{ row.hidden ? t('modelDegradation.admin.hidden') : t('modelDegradation.admin.visible') }}
                </button>
              </li>
            </ul>
          </section>
        </template>
      </div>
    </div>
  </AppLayout>
</template>

<style scoped>
.ma-admin-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 18px;
  border: 1px solid var(--ma-border);
  border-radius: 16px;
  background: var(--ma-panel);
  backdrop-filter: blur(10px);
}

.ma-admin-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.ma-admin-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--ma-control-fg);
}

.ma-admin-sub {
  font-size: 11px;
  color: var(--ma-faint);
  font-variant-numeric: tabular-nums;
}

.ma-admin-hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--ma-muted);
}

.ma-admin-empty {
  padding: 14px;
  border: 1px dashed var(--ma-border);
  border-radius: 10px;
  color: var(--ma-faint);
  font-size: 12px;
  text-align: center;
}

.ma-admin-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.ma-admin-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 9px 12px;
  border: 1px solid var(--ma-border-soft);
  border-radius: 10px;
  background: var(--ma-panel-soft);
}

.ma-admin-row-main {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
}

.ma-admin-host {
  overflow: hidden;
  font-size: 13px;
  font-weight: 500;
  color: var(--ma-control-fg);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ma-admin-count {
  font-size: 11px;
  color: var(--ma-faint);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.ma-toggle {
  flex: 0 0 auto;
  min-width: 60px;
  padding: 4px 12px;
  border: 1px solid var(--ma-input-border);
  border-radius: 999px;
  background: var(--ma-input-bg);
  color: var(--ma-label);
  font-size: 11px;
  cursor: pointer;
  transition: all 0.14s ease;
}

.ma-toggle:hover:not(:disabled) {
  border-color: var(--ma-primary);
}

.ma-toggle.is-on {
  border-color: var(--ma-fail);
  background: color-mix(in srgb, var(--ma-fail) 12%, transparent);
  color: var(--ma-fail);
  font-weight: 600;
}

.ma-toggle:disabled {
  opacity: 0.5;
  cursor: wait;
}

.ma-banner {
  margin: 0;
  padding: 9px 12px;
  border-radius: 10px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--ma-muted);
}

.ma-banner.is-error {
  background: color-mix(in srgb, var(--ma-fail) 12%, transparent);
  color: var(--ma-fail);
}
</style>
