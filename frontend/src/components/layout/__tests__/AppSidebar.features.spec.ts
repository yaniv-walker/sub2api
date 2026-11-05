import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'

const state = vi.hoisted(() => ({
  app: { cachedPublicSettings: { tutorial_enabled: true, model_degradation_enabled: true }, backendModeEnabled: false, publicSettingsLoaded: true, sidebarScrollTop: 0 },
  auth: { isAdmin: false, isSimpleMode: false }
}))
vi.mock('@/stores', () => ({
  useAppStore: () => state.app,
  useAuthStore: () => state.auth,
  useAdminSettingsStore: () => ({ fetch: vi.fn(), customMenuItems: [] }),
  useOnboardingStore: () => ({ isCurrentStep: () => false })
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('vue-router', () => ({ useRoute: () => ({ path: '/dashboard' }), useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: false, refreshBatchImageAccess: vi.fn() }) }))
vi.mock('@/components/common/VersionBadge.vue', () => ({ default: { template: '<span />' } }))
import AppSidebar from '../AppSidebar.vue'

beforeEach(() => {
  state.app = reactive({ cachedPublicSettings: { tutorial_enabled: true, model_degradation_enabled: true }, backendModeEnabled: false, publicSettingsLoaded: true, sidebarScrollTop: 0 })
  state.auth = reactive({ isAdmin: false, isSimpleMode: false })
})
function sidebar() {
  return mount(AppSidebar, { global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } })
}
describe('sidebar feature visibility', () => {
  it('puts the enabled tutorial directly below the dashboard for regular users', () => {
    const wrapper = sidebar()
    const links = wrapper.findAll('nav a').map(a => a.attributes('href'))
    expect(links.slice(0, 2)).toEqual(['/dashboard', '/tutorial'])
    wrapper.unmount()
  })
  it('updates tutorial visibility when its switch changes and retains it in simple mode', async () => {
    const wrapper = sidebar()
    state.auth.isSimpleMode = true
    await nextTick()
    expect(wrapper.find('a[href="/tutorial"]').exists()).toBe(true)
    state.app.cachedPublicSettings.tutorial_enabled = false
    await nextTick()
    expect(wrapper.find('a[href="/tutorial"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it.each([false, true])('gates the detection entry for admin=%s with the same switch', async (admin) => {
    state.auth.isAdmin = admin
    const wrapper = sidebar()
    const path = admin ? '/admin/model-degradation' : '/model-degradation'
    expect(wrapper.find(`a[href="${path}"]`).exists()).toBe(true)
    state.app.cachedPublicSettings.model_degradation_enabled = false
    await nextTick()
    expect(wrapper.find(`a[href="${path}"]`).exists()).toBe(false)
    expect(wrapper.find('a[href="/admin/settings"]').exists()).toBe(admin)
    wrapper.unmount()
  })
})
