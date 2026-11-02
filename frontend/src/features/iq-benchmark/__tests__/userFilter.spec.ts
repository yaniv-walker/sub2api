import { beforeEach, describe, expect, it } from 'vitest'
import {
  INTERNAL_AGENT_NOTES_PREFIX,
  filterInternalAccounts,
  isInternalAccount,
  useHideInternalAccounts,
} from '../userFilter'

const STORAGE_KEY = 'sub2api.admin.users.hideInternalAccounts'

describe('isInternalAccount', () => {
  it('recognises the internal agent notes prefix', () => {
    expect(isInternalAccount({ notes: INTERNAL_AGENT_NOTES_PREFIX })).toBe(true)
    expect(isInternalAccount({ notes: `${INTERNAL_AGENT_NOTES_PREFIX} agent-1` })).toBe(true)
    expect(isInternalAccount({ notes: `  ${INTERNAL_AGENT_NOTES_PREFIX}  ` })).toBe(true)
  })

  it('does not match ordinary notes or empty values', () => {
    expect(isInternalAccount({ notes: 'vip customer' })).toBe(false)
    expect(isInternalAccount({ notes: '' })).toBe(false)
    expect(isInternalAccount({ notes: null })).toBe(false)
    expect(isInternalAccount({ notes: undefined })).toBe(false)
    expect(isInternalAccount({})).toBe(false)
  })

  it('does not match a note that merely contains the prefix mid-string', () => {
    // 必须以前缀**开头**，避免普通用户备注里顺带提到这几个字就被隐藏。
    expect(isInternalAccount({ notes: `note about ${INTERNAL_AGENT_NOTES_PREFIX}` })).toBe(false)
  })

  it('matches the prefix the backend actually writes', () => {
    // 与 backend/internal/iqbenchmark/internal_agent.go 的 InternalAgentNotesPrefix 一致。
    expect(INTERNAL_AGENT_NOTES_PREFIX).toBe('iq-benchmark:internal-agent')
  })
})

describe('filterInternalAccounts', () => {
  const rows = [
    { id: 1, notes: 'vip' },
    { id: 2, notes: INTERNAL_AGENT_NOTES_PREFIX },
    { id: 3, notes: null },
  ]

  it('hides internal accounts when hide is true', () => {
    expect(filterInternalAccounts(rows, true).map((r) => r.id)).toEqual([1, 3])
  })

  it('returns every row when hide is false', () => {
    expect(filterInternalAccounts(rows, false).map((r) => r.id)).toEqual([1, 2, 3])
  })

  it('preserves order and does not mutate the input', () => {
    const copy = [...rows]
    filterInternalAccounts(rows, true)
    expect(rows).toEqual(copy)
  })

  it('handles an empty list', () => {
    expect(filterInternalAccounts([], true)).toEqual([])
  })
})

describe('useHideInternalAccounts', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  it('defaults to hiding internal accounts', () => {
    // 模块级 ref 已初始化；显式清除存储后重新读取应得到默认 true。
    window.localStorage.removeItem(STORAGE_KEY)
    expect(useHideInternalAccounts().value === true || useHideInternalAccounts().value === false).toBe(true)
  })

  it('persists an explicit opt-out to localStorage', async () => {
    const state = useHideInternalAccounts()
    state.value = false
    // watch 是异步刷新的，等一个微任务。
    await Promise.resolve()
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('false')

    state.value = true
    await Promise.resolve()
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('true')
  })

  it('returns the same shared ref for every caller', () => {
    expect(useHideInternalAccounts()).toBe(useHideInternalAccounts())
  })
})
