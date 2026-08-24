import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RedeemView from '../RedeemView.vue'

const { getHistory, getPublicSettings, appState } = vi.hoisted(() => ({
  getHistory: vi.fn(),
  getPublicSettings: vi.fn(),
  appState: {
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { balance: 0, concurrency: 1 },
    refreshUser: vi.fn(),
  }),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appState,
}))

vi.mock('@/stores/subscriptions', () => ({
  useSubscriptionStore: () => ({ fetchActiveSubscriptions: vi.fn() }),
}))

vi.mock('@/api', () => ({
  redeemAPI: {
    getHistory,
    redeem: vi.fn(),
  },
  authAPI: {
    getPublicSettings,
  },
}))

describe('RedeemView purchase link', () => {
  beforeEach(() => {
    getHistory.mockReset()
    getPublicSettings.mockReset()
    getHistory.mockResolvedValue([])
  })

  // 验证启用购买入口且地址安全时，页面展示可在新窗口打开的购买按钮。
  it('shows the purchase button for a configured HTTPS URL', async () => {
    getPublicSettings.mockResolvedValue({
      contact_info: '',
      purchase_subscription_enabled: true,
      purchase_subscription_url: 'https://shop.example.com/products',
    })

    const wrapper = mount(RedeemView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
        },
      },
    })
    await flushPromises()

    const link = wrapper.get('[data-testid="purchase-redeem-code"]')
    expect(link.attributes('href')).toBe('https://shop.example.com/products')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toContain('noopener')
  })

  // 验证危险协议不会进入链接属性，避免后台误配置产生脚本执行入口。
  it('hides the purchase button for an unsafe URL', async () => {
    getPublicSettings.mockResolvedValue({
      contact_info: '',
      purchase_subscription_enabled: true,
      purchase_subscription_url: 'javascript:alert(1)',
    })

    const wrapper = mount(RedeemView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
        },
      },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="purchase-redeem-code"]').exists()).toBe(false)
  })
})
