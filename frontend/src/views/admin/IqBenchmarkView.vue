<template>
  <AppLayout>
    <div class="mx-auto flex w-full max-w-7xl flex-col gap-4 p-4">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
            {{ t('iqBenchmark.admin.title') }}
          </h1>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('iqBenchmark.subtitle') }}</p>
        </div>
        <button class="btn btn-secondary btn-sm" :disabled="loading" @click="loadAll">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
        </button>
      </header>

      <!-- 未启用提示：让管理员立刻明白为什么用户侧看不到页面 -->
      <div
        v-if="settings && !settings.enabled"
        class="rounded-lg bg-amber-50 p-3 text-xs text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
      >
        <p class="font-semibold">{{ t('iqBenchmark.admin.disabledNotice') }}</p>
        <p class="mt-1">{{ t('iqBenchmark.admin.enableHint') }}</p>
      </div>

      <!-- 后台接口在未启用时返回 503：单独提示，避免误以为是故障 -->
      <div
        v-if="disabledByBackend"
        class="rounded-lg bg-amber-50 p-3 text-xs text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
      >
        {{ t('iqBenchmark.error.disabled') }}
      </div>

      <nav class="flex gap-1 border-b border-gray-200 dark:border-dark-700">
        <button
          v-for="tab in tabs"
          :key="tab"
          class="border-b-2 px-3 py-2 text-sm font-medium transition"
          :class="
            activeTab === tab
              ? 'border-primary-500 text-primary-600 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:text-gray-700 dark:hover:text-gray-300'
          "
          @click="activeTab = tab"
        >
          {{ t(`iqBenchmark.admin.tab.${tab}`) }}
        </button>
      </nav>

      <!-- ============ 任务 ============ -->
      <section v-show="activeTab === 'tasks'" class="flex flex-col gap-3">
        <div class="flex justify-end">
          <button class="btn btn-primary btn-sm" @click="openTaskForm(null)">
            <Icon name="plus" size="sm" />
            {{ t('iqBenchmark.admin.tasks.create') }}
          </button>
        </div>

        <div class="card overflow-x-auto">
          <table class="w-full text-xs">
            <thead>
              <tr class="border-b border-gray-200 text-left text-gray-400 dark:border-dark-700">
                <th class="p-2">{{ t('iqBenchmark.admin.tasks.name') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.tasks.model') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.tasks.status') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.tasks.interval') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.tasks.nextRun') }}</th>
                <th class="p-2 text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="task in tasks"
                :key="task.id"
                class="border-b border-gray-100 last:border-0 dark:border-dark-800"
              >
                <td class="p-2">
                  <div class="font-medium">{{ task.name }}</div>
                  <div class="text-gray-400">
                    {{ t('iqBenchmark.admin.tasks.group') }} #{{ task.group_id }}
                  </div>
                </td>
                <td class="p-2">{{ task.model }}</td>
                <td class="p-2">
                  <span class="rounded-full px-2 py-0.5 text-[10px] font-semibold" :class="taskStatusClass(task)">
                    {{ t(`iqBenchmark.status.${task.status}`) }}
                  </span>
                  <div v-if="task.status_reason" class="mt-1 max-w-[16rem] truncate text-[10px] text-gray-400">
                    {{ task.status_reason }}
                  </div>
                </td>
                <td class="p-2 tabular-nums">{{ task.interval_minutes }} min</td>
                <td class="p-2 tabular-nums">{{ formatTime(task.next_run_at) }}</td>
                <td class="p-2">
                  <div class="flex items-center justify-end gap-1">
                    <button
                      class="btn btn-ghost btn-sm py-0.5"
                      :title="t('iqBenchmark.admin.tasks.runNow')"
                      @click="runNow(task)"
                    >
                      <Icon name="play" size="xs" />
                    </button>
                    <button
                      class="btn btn-ghost btn-sm py-0.5"
                      :title="task.enabled ? t('iqBenchmark.state.off') : t('iqBenchmark.state.on')"
                      @click="toggleTask(task)"
                    >
                      <Icon :name="task.enabled ? 'eyeOff' : 'check'" size="xs" />
                    </button>
                    <button class="btn btn-ghost btn-sm py-0.5" @click="openTaskForm(task)">
                      <Icon name="edit" size="xs" />
                    </button>
                    <button class="btn btn-ghost btn-sm py-0.5" @click="removeTask(task)">
                      <Icon name="trash" size="xs" />
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="tasks.length === 0">
                <td colspan="6" class="p-6 text-center text-gray-400">{{ t('iqBenchmark.empty.tasks') }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- 选中任务的执行记录 -->
        <div v-if="runsTaskId !== null" class="card p-3">
          <div class="mb-2 flex items-center justify-between">
            <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-400">
              {{ t('iqBenchmark.admin.runs.title') }}
            </h3>
            <button class="btn btn-ghost btn-sm py-0.5" @click="runsTaskId = null">
              <Icon name="x" size="xs" />
            </button>
          </div>
          <RunTable :runs="runs" />
        </div>
      </section>

      <!-- ============ 题目 ============ -->
      <section v-show="activeTab === 'cases'" class="flex flex-col gap-3">
        <div class="flex justify-end">
          <button class="btn btn-primary btn-sm" @click="openCaseForm(null)">
            <Icon name="plus" size="sm" />
            {{ t('iqBenchmark.admin.cases.create') }}
          </button>
        </div>

        <div class="card overflow-x-auto">
          <table class="w-full text-xs">
            <thead>
              <tr class="border-b border-gray-200 text-left text-gray-400 dark:border-dark-700">
                <th class="p-2">{{ t('iqBenchmark.admin.cases.name') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.cases.kind') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.cases.expectKind') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.cases.sortOrder') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.cases.enabled') }}</th>
                <th class="p-2 text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="item in cases"
                :key="item.id"
                class="border-b border-gray-100 last:border-0 dark:border-dark-800"
              >
                <td class="p-2">
                  <div class="font-medium">{{ item.name }}</div>
                  <div class="max-w-[24rem] truncate text-gray-400">{{ item.prompt }}</div>
                </td>
                <td class="p-2">{{ t(`iqBenchmark.kind.${item.kind}`) }}</td>
                <td class="p-2">{{ t(`iqBenchmark.kind.${item.expect_kind}`) }}</td>
                <td class="p-2 tabular-nums">{{ item.sort_order }}</td>
                <td class="p-2">
                  <span
                    class="rounded-full px-2 py-0.5 text-[10px] font-semibold"
                    :class="
                      item.enabled
                        ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                        : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400'
                    "
                  >
                    {{ item.enabled ? t('iqBenchmark.state.on') : t('iqBenchmark.state.off') }}
                  </span>
                  <span v-if="item.builtin" class="ml-1 text-[10px] text-gray-400">
                    {{ t('iqBenchmark.admin.cases.builtin') }}
                  </span>
                </td>
                <td class="p-2">
                  <div class="flex items-center justify-end gap-1">
                    <button class="btn btn-ghost btn-sm py-0.5" @click="openCaseForm(item)">
                      <Icon name="edit" size="xs" />
                    </button>
                    <!-- 内置题目不可删除：不渲染按钮而不是渲染禁用态 -->
                    <button
                      v-if="!item.builtin"
                      class="btn btn-ghost btn-sm py-0.5"
                      @click="removeCase(item)"
                    >
                      <Icon name="trash" size="xs" />
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="cases.length === 0">
                <td colspan="6" class="p-6 text-center text-gray-400">{{ t('iqBenchmark.empty.cases') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- ============ 作品 ============ -->
      <section v-show="activeTab === 'artifacts'" class="flex flex-col gap-3">
        <div class="card flex flex-wrap items-center gap-2 p-2">
          <input
            v-model="artifactFilters.model"
            class="input h-9 min-w-[9rem] py-0 text-xs"
            :placeholder="t('iqBenchmark.filter.model')"
            @input="reloadAdminArtifacts"
          />
          <select v-model="artifactFilters.status" class="input h-9 py-0 text-xs" @change="reloadAdminArtifacts">
            <option value="">{{ t('iqBenchmark.filter.statusAll') }}</option>
            <option value="success">{{ t('iqBenchmark.status.success') }}</option>
            <option value="failed">{{ t('iqBenchmark.status.failed') }}</option>
            <option value="timeout">{{ t('iqBenchmark.status.timeout') }}</option>
            <option value="blocked">{{ t('iqBenchmark.status.blocked') }}</option>
          </select>
        </div>

        <div class="card overflow-x-auto">
          <table class="w-full text-xs">
            <thead>
              <tr class="border-b border-gray-200 text-left text-gray-400 dark:border-dark-700">
                <th class="p-2">ID</th>
                <th class="p-2">{{ t('iqBenchmark.admin.artifacts.case') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.artifacts.model') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.artifacts.status') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.artifacts.unsafeFlags') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.artifacts.cost') }}</th>
                <th class="p-2">{{ t('iqBenchmark.admin.artifacts.score') }}</th>
                <th class="p-2 text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="item in adminArtifacts"
                :key="item.id"
                class="border-b border-gray-100 last:border-0 dark:border-dark-800"
              >
                <td class="p-2 tabular-nums">{{ item.id }}</td>
                <td class="p-2 max-w-[14rem] truncate">{{ caseName(item) }}</td>
                <td class="p-2">{{ item.model }}</td>
                <td class="p-2">
                  {{ t(`iqBenchmark.status.${item.status}`) }}
                  <div v-if="item.error_code" class="text-[10px] text-red-500">{{ item.error_code }}</div>
                </td>
                <td class="p-2">
                  <span v-if="item.unsafe_flags.length === 0" class="text-gray-400">—</span>
                  <span
                    v-for="flag in item.unsafe_flags"
                    :key="flag"
                    class="mr-1 rounded bg-amber-100 px-1 text-[10px] text-amber-700 dark:bg-amber-900/40 dark:text-amber-300"
                  >{{ flag }}</span>
                </td>
                <td class="p-2 tabular-nums">{{ formatCost(item.cost) }}</td>
                <td class="p-2 tabular-nums">{{ item.score ?? '—' }}</td>
                <td class="p-2">
                  <div class="flex items-center justify-end gap-1">
                    <button class="btn btn-ghost btn-sm py-0.5" @click="promptScore(item)">
                      <Icon name="check" size="xs" />
                    </button>
                    <button class="btn btn-ghost btn-sm py-0.5" @click="removeArtifact(item)">
                      <Icon name="trash" size="xs" />
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="adminArtifacts.length === 0">
                <td colspan="8" class="p-6 text-center text-gray-400">{{ t('iqBenchmark.empty.artifacts') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- ============ 设置 ============ -->
      <section v-show="activeTab === 'settings'" class="card p-4">
        <SettingsPanel
          :initial="settings"
          :saving="savingSettings"
          :saved="settingsSaved"
          @submit="saveSettingsForm"
        />
      </section>
    </div>

    <!-- 任务表单弹层 -->
    <Teleport to="body">
      <div
        v-if="taskFormOpen"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
        @click.self="taskFormOpen = false"
      >
        <div class="card max-h-[90vh] w-full max-w-2xl overflow-auto p-4">
          <h3 class="mb-3 text-sm font-semibold">
            {{ taskEditing ? t('iqBenchmark.admin.tasks.edit') : t('iqBenchmark.admin.tasks.create') }}
          </h3>
          <TaskForm
            :initial="taskEditing"
            :case-options="cases"
            :saving="savingTask"
            :interval-min="settings?.interval_min_minutes ?? 15"
            :interval-max="settings?.interval_max_minutes ?? 10080"
            @submit="submitTaskForm"
            @cancel="taskFormOpen = false"
          />
        </div>
      </div>
    </Teleport>

    <!-- 题目表单弹层 -->
    <Teleport to="body">
      <div
        v-if="caseFormOpen"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
        @click.self="caseFormOpen = false"
      >
        <div class="card max-h-[90vh] w-full max-w-2xl overflow-auto p-4">
          <h3 class="mb-3 text-sm font-semibold">
            {{ caseEditing ? t('iqBenchmark.admin.cases.edit') : t('iqBenchmark.admin.cases.create') }}
          </h3>
          <CaseForm
            :initial="caseEditing"
            :saving="savingCase"
            @submit="submitCaseForm"
            @cancel="caseFormOpen = false"
          />
        </div>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import CaseForm from '@/features/iq-benchmark/CaseForm.vue'
import RunTable from '@/features/iq-benchmark/RunTable.vue'
import SettingsPanel from '@/features/iq-benchmark/SettingsPanel.vue'
import TaskForm from '@/features/iq-benchmark/TaskForm.vue'
import {
  createCase,
  createTask,
  deleteArtifact,
  deleteCase,
  deleteTask,
  fetchAdminArtifacts,
  fetchAdminCases,
  fetchAdminTasks,
  fetchRuns,
  fetchSettings,
  runTaskNow,
  saveSettings,
  scoreArtifact,
  setTaskEnabled,
  updateCase,
  updateTask,
} from '@/features/iq-benchmark/api'
import { formatCost } from '@/features/iq-benchmark/filters'
import {
  probeIqGalleryAvailability,
  resetIqGalleryAvailability,
} from '@/features/iq-benchmark/availability'
import { parseRateLimitError } from '@/features/iq-benchmark/rateLimit'
import type {
  IQAdminArtifact,
  IQAdminCase,
  IQAdminRun,
  IQAdminTask,
  IQArtifactFilters,
  IQBenchmarkSettings,
  IQCaseInput,
  IQTaskInput,
} from '@/features/iq-benchmark/types'

const { t, locale } = useI18n()

const tabs = ['tasks', 'cases', 'artifacts', 'settings'] as const
type Tab = (typeof tabs)[number]
const activeTab = ref<Tab>('tasks')

const loading = ref(false)
/** 后台返回 503（功能未启用）时置位。 */
const disabledByBackend = ref(false)

const tasks = ref<IQAdminTask[]>([])
const cases = ref<IQAdminCase[]>([])
const adminArtifacts = ref<IQAdminArtifact[]>([])
const runs = ref<IQAdminRun[]>([])
const runsTaskId = ref<number | null>(null)
const settings = ref<IQBenchmarkSettings | null>(null)

const artifactFilters = ref<IQArtifactFilters>({ model: '', status: '', page: 1, page_size: 50 })

const taskFormOpen = ref(false)
const taskEditing = ref<IQAdminTask | null>(null)
const savingTask = ref(false)

const caseFormOpen = ref(false)
const caseEditing = ref<IQAdminCase | null>(null)
const savingCase = ref(false)

const savingSettings = ref(false)
const settingsSaved = ref(false)

/** 统一的错误处理：503 记一次以便顶部提示，其余静默（列表本身会显示空态）。 */
function handleError(err: unknown): void {
  const info = parseRateLimitError(err)
  if (info.featureDisabled) {
    disabledByBackend.value = true
  }
}

async function loadAll(): Promise<void> {
  loading.value = true
  disabledByBackend.value = false
  // 配置最先加载：它决定其余请求是否可能 503。
  try {
    settings.value = await fetchSettings()
  } catch (err) {
    handleError(err)
  }
  const results = await Promise.allSettled([
    fetchAdminTasks(),
    fetchAdminCases(),
    fetchAdminArtifacts(artifactFilters.value),
  ])
  if (results[0].status === 'fulfilled') tasks.value = results[0].value.items
  else handleError(results[0].reason)
  if (results[1].status === 'fulfilled') cases.value = results[1].value.items
  else handleError(results[1].reason)
  if (results[2].status === 'fulfilled') adminArtifacts.value = results[2].value.items
  else handleError(results[2].reason)
  loading.value = false
}

onMounted(loadAll)

function caseName(item: IQAdminArtifact): string {
  const snapshot = item.case_snapshot as { name?: string } | undefined
  return snapshot?.name ?? `#${item.case_id}`
}

function formatTime(raw: string | undefined): string {
  if (!raw) return '—'
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/** 熔断态用红/琥珀标出，运行中单独用蓝色，避免与失败混淆。 */
function taskStatusClass(task: IQAdminTask): string {
  if (task.status === 'running') {
    return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'
  }
  if (task.status === 'idle') {
    return task.enabled
      ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
      : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400'
  }
  return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
}

// ---------------- 任务 ----------------

function openTaskForm(task: IQAdminTask | null): void {
  taskEditing.value = task
  taskFormOpen.value = true
}

async function submitTaskForm(input: IQTaskInput): Promise<void> {
  savingTask.value = true
  try {
    if (taskEditing.value) {
      await updateTask(taskEditing.value.id, input)
    } else {
      await createTask(input)
    }
    taskFormOpen.value = false
    const page = await fetchAdminTasks()
    tasks.value = page.items
  } catch (err) {
    handleError(err)
  } finally {
    savingTask.value = false
  }
}

async function toggleTask(task: IQAdminTask): Promise<void> {
  try {
    await setTaskEnabled(task.id, !task.enabled)
    const page = await fetchAdminTasks()
    tasks.value = page.items
  } catch (err) {
    handleError(err)
  }
}

async function runNow(task: IQAdminTask): Promise<void> {
  try {
    await runTaskNow(task.id)
    runsTaskId.value = task.id
    const page = await fetchRuns(task.id)
    runs.value = page.items
  } catch (err) {
    handleError(err)
  }
}

async function removeTask(task: IQAdminTask): Promise<void> {
  if (!window.confirm(t('iqBenchmark.admin.tasks.deleteConfirm'))) return
  try {
    await deleteTask(task.id)
    tasks.value = tasks.value.filter((item) => item.id !== task.id)
  } catch (err) {
    handleError(err)
  }
}

// ---------------- 题目 ----------------

function openCaseForm(item: IQAdminCase | null): void {
  caseEditing.value = item
  caseFormOpen.value = true
}

async function submitCaseForm(input: IQCaseInput): Promise<void> {
  savingCase.value = true
  try {
    if (caseEditing.value) {
      await updateCase(caseEditing.value.id, input)
    } else {
      await createCase(input)
    }
    caseFormOpen.value = false
    const page = await fetchAdminCases()
    cases.value = page.items
  } catch (err) {
    handleError(err)
  } finally {
    savingCase.value = false
  }
}

async function removeCase(item: IQAdminCase): Promise<void> {
  if (!window.confirm(t('iqBenchmark.admin.cases.deleteConfirm'))) return
  try {
    await deleteCase(item.id)
    cases.value = cases.value.filter((row) => row.id !== item.id)
  } catch (err) {
    handleError(err)
  }
}

// ---------------- 作品 ----------------

async function reloadAdminArtifacts(): Promise<void> {
  try {
    const page = await fetchAdminArtifacts(artifactFilters.value)
    adminArtifacts.value = page.items
  } catch (err) {
    handleError(err)
  }
}

async function promptScore(item: IQAdminArtifact): Promise<void> {
  const raw = window.prompt(t('iqBenchmark.admin.artifacts.scorePrompt'), String(item.score ?? 0))
  if (raw === null) return
  const value = Number(raw)
  if (!Number.isFinite(value) || value < 0 || value > 100) return
  try {
    const updated = await scoreArtifact(item.id, value)
    if (updated) {
      // 就地更新，避免整表重载打扰管理员当前的操作位置。
      const index = adminArtifacts.value.findIndex((row) => row.id === item.id)
      if (index >= 0) adminArtifacts.value[index] = updated
    }
  } catch (err) {
    handleError(err)
  }
}

async function removeArtifact(item: IQAdminArtifact): Promise<void> {
  if (!window.confirm(t('iqBenchmark.admin.artifacts.deleteConfirm'))) return
  try {
    await deleteArtifact(item.id)
    adminArtifacts.value = adminArtifacts.value.filter((row) => row.id !== item.id)
  } catch (err) {
    handleError(err)
  }
}

// ---------------- 设置 ----------------

async function saveSettingsForm(next: IQBenchmarkSettings): Promise<void> {
  savingSettings.value = true
  settingsSaved.value = false
  const wasEnabled = settings.value?.enabled === true
  try {
    const saved = await saveSettings(next)
    if (saved) settings.value = saved
    settingsSaved.value = true
    // 开关状态一旦变化就重新探测：让侧栏菜单立刻出现/消失，
    // 并让后台的 503 状态与真实开关一致。
    const nowEnabled = settings.value?.enabled === true
    if (nowEnabled !== wasEnabled) {
      resetIqGalleryAvailability()
      void probeIqGalleryAvailability(true)
      disabledByBackend.value = false
    }
    setTimeout(() => {
      settingsSaved.value = false
    }, 2500)
  } catch (err) {
    handleError(err)
  } finally {
    savingSettings.value = false
  }
}
</script>
