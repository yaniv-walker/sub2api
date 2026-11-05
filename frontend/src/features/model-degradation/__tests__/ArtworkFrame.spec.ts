import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ArtworkFrame from '../components/ArtworkFrame.vue'

describe('ArtworkFrame zoom', () => {
  it('固定像素 SVG 补齐 viewBox，在受限视口内完整等比展示且继续隔离脚本', () => {
    const wrapper = mount(ArtworkFrame, { props: { source: '<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="800"><rect width="1200" height="800" /></svg>', zoom: true } })
    const frame = wrapper.get('.ma-artwork-frame')
    const doc = new DOMParser().parseFromString(frame.attributes('srcdoc'), 'text/html')
    expect(doc.querySelector('svg')?.getAttribute('viewBox')).toBe('0 0 1200 800')
    expect(doc.querySelector('svg')?.getAttribute('preserveAspectRatio')).toBe('xMidYMid meet')
    expect(frame.attributes('sandbox')).toBe('')
    expect(frame.attributes('style')).toContain('80dvh')
    expect(doc.querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute('content')).toContain("default-src 'none'")
    wrapper.unmount()
  })

  it('保留原有 viewBox，不把百分比尺寸误当像素尺寸', () => {
    const wrapper = mount(ArtworkFrame, { props: { source: '<svg xmlns="http://www.w3.org/2000/svg" width="100%" height="100%" viewBox="0 0 600 1000"></svg>', zoom: true } })
    expect(wrapper.get('.ma-artwork-frame').attributes('srcdoc')).toContain('viewBox="0 0 600 1000"')
    wrapper.unmount()
  })
})
