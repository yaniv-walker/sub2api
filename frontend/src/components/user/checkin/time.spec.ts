import { describe, expect, it } from 'vitest'
import { formatCheckinTime } from './time'
describe('Beijing activity time', () => {
  it('uses the Beijing date across UTC midnight regardless of browser timezone', () => {
    expect(formatCheckinTime('2030-01-01T16:00:00Z', 'zh')).toBe('2030/01/02 00:00:00')
    expect(formatCheckinTime('2030-01-02T00:00:00+08:00', 'en')).toBe('01/02/2030, 00:00:00')
  })
  it('shows a neutral placeholder for an unavailable timestamp', () => {
    expect(formatCheckinTime('')).toBe('—')
  })
})
