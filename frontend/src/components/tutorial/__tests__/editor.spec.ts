import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Editor } from '@tiptap/core'
const api = vi.hoisted(() => ({ listTutorialAssets:vi.fn(), uploadTutorialAsset:vi.fn(), deleteTutorialAsset:vi.fn(), renameTutorialAsset:vi.fn() }))
vi.mock('@/api/tutorial', () => api)
import TutorialEditor from '../TutorialEditor.vue'

beforeEach(() => { api.listTutorialAssets.mockReset(); });
describe('visual media editing', () => {
  it('refreshes existing image and video node previews without changing document attributes', async () => {
    const asset = (kind:string, signature:string) => ({ id:kind === 'image' ? 1 : 2, kind, public_url:`/api/v1/tutorial-assets/${kind}`, preview_url:`/api/v1/tutorial-assets/${kind}?sig=${signature}`, label:kind, original_name:kind, size_bytes:100 });
    api.listTutorialAssets.mockResolvedValueOnce([asset('image','old'),asset('video','old')]).mockResolvedValueOnce([asset('image','renewed'),asset('video','renewed')]);
    const wrapper = mount(TutorialEditor, { props:{ tutorialId:1, documentVersion:1, modelValue:'<img src="/api/v1/tutorial-assets/image" data-width="50%"><video src="/api/v1/tutorial-assets/video" controls></video>' } });
    await flushPromises();
    const editor = (wrapper.vm as unknown as { editor:Editor }).editor;
    const before = editor.getJSON();
    const refresh = wrapper.findAll('button').find(button => button.text().startsWith('素材库'));
    await refresh!.trigger('click'); await flushPromises();
    expect(wrapper.find('.ProseMirror img').attributes('src')).toContain('sig=renewed');
    expect(wrapper.find('.ProseMirror video').attributes('src')).toContain('sig=renewed');
    expect(editor.getJSON()).toEqual(before);
    expect(editor.getHTML()).not.toContain('?sig=');
    expect(wrapper.emitted('update:modelValue')).toBeUndefined();
    wrapper.unmount();
  });
  it('reloads assets after saving a new document version so pasted cross-document media gains a preview', async () => {
    api.listTutorialAssets.mockResolvedValueOnce([]).mockResolvedValueOnce([{ id:3, kind:'image', public_url:'/api/v1/tutorial-assets/shared', preview_url:'/api/v1/tutorial-assets/shared?sig=new-link', label:'shared', original_name:'shared.png', size_bytes:100 }]);
    const wrapper = mount(TutorialEditor, { props:{ tutorialId:1, documentVersion:1, modelValue:'<img src="/api/v1/tutorial-assets/shared">' } });
    await flushPromises();
    const editor = (wrapper.vm as unknown as { editor:Editor }).editor;
    const before = editor.getJSON();
    await wrapper.setProps({ documentVersion:2 }); await flushPromises();
    expect(wrapper.find('.ProseMirror img').attributes('src')).toContain('sig=new-link');
    expect(editor.getJSON()).toEqual(before);
    expect(editor.getHTML()).not.toContain('?sig=');
    wrapper.unmount();
  });
  it('renders a signed preview but persists only the stable unsigned asset reference', async () => {
    api.listTutorialAssets.mockResolvedValue([{ id:1, kind:'image', public_url:'/api/v1/tutorial-assets/image', preview_url:'/api/v1/tutorial-assets/image?sig=temporary', label:'截图', original_name:'image.png', size_bytes:1234 }])
    const wrapper = mount(TutorialEditor, { props:{ tutorialId:1, modelValue:'## 配置\n\n<img src="/api/v1/tutorial-assets/image" alt="截图" data-width="50%">' } })
    await flushPromises()
    const editor = (wrapper.vm as unknown as { editor:Editor }).editor
    expect(wrapper.find('.ProseMirror img').attributes('src')).toContain('?sig=temporary')
    editor.commands.insertContent('已编辑')
    await flushPromises()
    const changes = wrapper.emitted('update:modelValue')
    expect(changes?.length).toBeGreaterThan(0)
    const markdown = String(changes![changes!.length - 1]![0])
    expect(markdown).toContain('/api/v1/tutorial-assets/image')
    expect(markdown).toContain('data-width="50%"')
    expect(markdown).not.toContain('?sig=')
    wrapper.unmount()
  })
})
