import { describe, expect, it } from 'vitest'
import {
  MAX_COMPARE_ITEMS,
  defaultFilterState,
  deriveModelOptions,
  filterSignature,
  formatCost,
  formatDuration,
  formatPercent,
  formatTokens,
  groupByCase,
  isDefaultFilterState,
  isSelected,
  successRate,
  toArtifactFilters,
  toggleCompareSelection,
} from '../filters'
import type { IQPublicArtifact } from '../types'

function artifact(overrides: Partial<IQPublicArtifact> = {}): IQPublicArtifact {
  return {
    id: 1,
    case_id: 10,
    case_name: 'Prompt A',
    case_kind: 'text',
    model: 'gpt-4o',
    render_kind: 'text',
    content: 'hello',
    status: 'success',
    duration_ms: 1200,
    is_unsafe: false,
    created_at: '2026-11-01T00:00:00Z',
    ...overrides,
  }
}

describe('defaultFilterState', () => {
  it('defaults to the last 7 days with no model/case/status/kind filter', () => {
    const state = defaultFilterState()
    expect(state).toEqual({ model: '', caseId: null, status: '', kind: '', sinceDays: 7 })
  })

  it('recognises the default state and rejects any deviation', () => {
    expect(isDefaultFilterState(defaultFilterState())).toBe(true)
    expect(isDefaultFilterState({ ...defaultFilterState(), model: 'x' })).toBe(false)
    expect(isDefaultFilterState({ ...defaultFilterState(), caseId: 3 })).toBe(false)
    expect(isDefaultFilterState({ ...defaultFilterState(), status: 'failed' })).toBe(false)
    expect(isDefaultFilterState({ ...defaultFilterState(), kind: 'svg' })).toBe(false)
    expect(isDefaultFilterState({ ...defaultFilterState(), sinceDays: 0 })).toBe(false)
  })
})

describe('toArtifactFilters', () => {
  const now = new Date('2026-11-02T12:00:00Z')

  it('maps a full filter state to query parameters', () => {
    const filters = toArtifactFilters(
      { model: 'gpt-4o', caseId: 7, status: 'success', kind: 'text', sinceDays: 7 },
      now,
      2,
      50,
    )
    expect(filters.model).toBe('gpt-4o')
    expect(filters.case_id).toBe(7)
    expect(filters.status).toBe('success')
    expect(filters.kind).toBe('text')
    expect(filters.page).toBe(2)
    expect(filters.page_size).toBe(50)
    // 7 天前 = 2026-10-26T12:00:00.000Z
    expect(filters.since).toBe('2026-10-26T12:00:00.000Z')
  })

  it('omits empty filters instead of sending empty strings', () => {
    const filters = toArtifactFilters({ ...defaultFilterState(), sinceDays: 0 }, now, 1, 20)
    expect(filters.model).toBeUndefined()
    expect(filters.case_id).toBeUndefined()
    expect(filters.status).toBeUndefined()
    expect(filters.kind).toBeUndefined()
    // sinceDays=0 表示不限时间，不得发送 since。
    expect(filters.since).toBeUndefined()
  })

  it('trims whitespace-only models away', () => {
    const filters = toArtifactFilters({ ...defaultFilterState(), model: '   ' }, now)
    expect(filters.model).toBeUndefined()
  })

  it('keeps case_id 0 out (only null is absent, 0 is a real id guard)', () => {
    // caseId 用 `!= null` 判空，因此 0 会被发送；这保证「有 id 就传」而不误判。
    const filters = toArtifactFilters({ ...defaultFilterState(), caseId: 0 }, now)
    expect(filters.case_id).toBe(0)
  })
})

describe('filterSignature', () => {
  it('is stable for equivalent states', () => {
    expect(filterSignature(defaultFilterState())).toBe(filterSignature(defaultFilterState()))
  })

  it('changes with any field', () => {
    const base = defaultFilterState()
    expect(filterSignature(base)).not.toBe(filterSignature({ ...base, model: 'x' }))
    expect(filterSignature(base)).not.toBe(filterSignature({ ...base, caseId: 1 }))
    expect(filterSignature(base)).not.toBe(filterSignature({ ...base, status: 'failed' }))
    expect(filterSignature(base)).not.toBe(filterSignature({ ...base, kind: 'svg' }))
    expect(filterSignature(base)).not.toBe(filterSignature({ ...base, sinceDays: 30 }))
  })

  it('ignores surrounding whitespace on the model', () => {
    const base = defaultFilterState()
    expect(filterSignature({ ...base, model: ' gpt ' })).toBe(filterSignature({ ...base, model: 'gpt' }))
  })
})

describe('deriveModelOptions', () => {
  it('returns a sorted unique list', () => {
    expect(deriveModelOptions(['zeta', 'alpha', 'zeta'])).toEqual(['alpha', 'zeta'])
  })

  it('drops empty and whitespace-only entries', () => {
    expect(deriveModelOptions(['', '  ', 'gpt'])).toEqual(['gpt'])
  })

  it('returns an empty list when nothing is provided', () => {
    expect(deriveModelOptions([])).toEqual([])
  })
})

describe('toggleCompareSelection', () => {
  it('adds an item and reports it as added', () => {
    const result = toggleCompareSelection([], 5)
    expect(result.selected).toEqual([5])
    expect(result.added).toBe(true)
    expect(result.reason).toBeUndefined()
    expect(isSelected(result.selected, 5)).toBe(true)
  })

  it('removes an already-selected item', () => {
    const result = toggleCompareSelection([5, 6], 5)
    expect(result.selected).toEqual([6])
    expect(result.added).toBe(false)
  })

  it('refuses to exceed the cap and keeps the previous selection', () => {
    const full = Array.from({ length: MAX_COMPARE_ITEMS }, (_, i) => i + 1)
    const result = toggleCompareSelection(full, 999)
    expect(result.reason).toBe('limit')
    expect(result.selected).toEqual(full)
    expect(result.added).toBe(false)
  })

  it('rejects invalid ids', () => {
    expect(toggleCompareSelection([], 0).reason).toBe('invalid')
    expect(toggleCompareSelection([], -1).reason).toBe('invalid')
    expect(toggleCompareSelection([], Number.NaN).reason).toBe('invalid')
  })

  it('still allows removal when the cap is reached', () => {
    const full = Array.from({ length: MAX_COMPARE_ITEMS }, (_, i) => i + 1)
    const result = toggleCompareSelection(full, 1)
    expect(result.added).toBe(false)
    expect(result.reason).toBeUndefined()
    expect(result.selected.length).toBe(MAX_COMPARE_ITEMS - 1)
  })

  it('does not mutate the input array', () => {
    const input = [1, 2]
    toggleCompareSelection(input, 3)
    expect(input).toEqual([1, 2])
  })

  it('exposes a cap of 8 to match the backend', () => {
    expect(MAX_COMPARE_ITEMS).toBe(8)
  })
})

describe('groupByCase', () => {
  it('groups artifacts by case and preserves order', () => {
    const groups = groupByCase([
      artifact({ id: 1, case_id: 10, case_name: 'A' }),
      artifact({ id: 2, case_id: 10, case_name: 'A' }),
      artifact({ id: 3, case_id: 20, case_name: 'B' }),
    ])
    expect(groups.map((g) => g.caseId)).toEqual([10, 20])
    expect(groups[0].caseName).toBe('A')
    expect(groups[0].items.map((i) => i.id)).toEqual([1, 2])
    expect(groups[1].items.map((i) => i.id)).toEqual([3])
  })

  it('returns an empty array for no artifacts', () => {
    expect(groupByCase([])).toEqual([])
  })
})

describe('formatDuration', () => {
  it('uses ms below one second', () => {
    expect(formatDuration(450)).toBe('450 ms')
    expect(formatDuration(999)).toBe('999 ms')
  })

  it('uses seconds below one minute', () => {
    expect(formatDuration(1000)).toBe('1.0 s')
    expect(formatDuration(1500)).toBe('1.5 s')
  })

  it('uses minutes and seconds above one minute', () => {
    expect(formatDuration(60_000)).toBe('1m 0s')
    expect(formatDuration(65_000)).toBe('1m 5s')
  })

  it('returns a dash for zero or invalid input', () => {
    expect(formatDuration(0)).toBe('—')
    expect(formatDuration(-1)).toBe('—')
    expect(formatDuration(Number.NaN)).toBe('—')
  })
})

describe('successRate / formatPercent', () => {
  it('returns null for an empty denominator instead of a misleading 0', () => {
    expect(successRate(0, 0)).toBeNull()
    expect(successRate(5, 0)).toBeNull()
    expect(formatPercent(successRate(0, 0))).toBe('—')
  })

  it('computes the ratio for a non-empty denominator', () => {
    expect(successRate(5, 10)).toBe(0.5)
    expect(successRate(10, 10)).toBe(1)
  })

  it('formats the ratio as a one-decimal percentage', () => {
    expect(formatPercent(0.5)).toBe('50.0%')
    expect(formatPercent(1)).toBe('100.0%')
    expect(formatPercent(0)).toBe('0.0%')
  })

  it('clamps out-of-range ratios', () => {
    expect(formatPercent(1.5)).toBe('100.0%')
    expect(formatPercent(-1)).toBe('0.0%')
  })
})

describe('formatCost', () => {
  it('returns a dash for missing values', () => {
    expect(formatCost(undefined)).toBe('—')
    expect(formatCost(Number.NaN)).toBe('—')
  })

  it('renders exact zero as 0', () => {
    expect(formatCost(0)).toBe('0')
  })

  it('flags sub-micro costs instead of rounding them to zero', () => {
    expect(formatCost(0.0000001)).toBe('<0.000001')
  })

  it('trims trailing zeros from the six-decimal form', () => {
    expect(formatCost(1.234567)).toBe('1.234567')
    expect(formatCost(0.5)).toBe('0.5')
    expect(formatCost(2)).toBe('2')
  })
})

describe('formatTokens', () => {
  it('returns a dash for missing values', () => {
    expect(formatTokens(undefined)).toBe('—')
    expect(formatTokens(Number.NaN)).toBe('—')
  })

  it('uses thousands separators', () => {
    expect(formatTokens(999)).toBe('999')
    expect(formatTokens(1500)).toBe('1,500')
    expect(formatTokens(1_234_567)).toBe('1,234,567')
  })

  it('renders zero', () => {
    expect(formatTokens(0)).toBe('0')
  })
})
