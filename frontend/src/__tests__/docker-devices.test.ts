import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import DevicePicker from '@/components/docker/DevicePicker.vue'
import TemplateDialog from '@/components/docker/TemplateDialog.vue'
import StackDialog from '@/components/docker/StackDialog.vue'
import { APP_TEMPLATES, defaultValues, renderTemplate } from '@/utils/appTemplates'
import { deviceSnippet } from '@/utils/deviceSnippet'
import type { HostDevice } from '@/api/docker'

vi.mock('@/components/docker/YamlEditor.vue', async () => {
  const { defineComponent, h } = await import('vue')
  return {
    __esModule: true,
    default: defineComponent({
      props: ['modelValue'],
      emits: ['update:modelValue'],
      setup: (props, { emit }) => () =>
        h('textarea', { 'data-test': 'compose', value: props.modelValue, onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLTextAreaElement).value) }),
    }),
  }
})

const plugins = () => [createPinia(), PrimeVue, ToastService]
afterEach(() => vi.unstubAllGlobals())

const gpu: HostDevice = { kind: 'gpu', path: '/dev/dri/renderD128', name: 'Intel HD Graphics', gid: 991, group: 'render' }
const zigbee: HostDevice = { kind: 'serial', path: '/dev/serial/by-id/usb-Zigbee-if00', name: 'Zigbee-if00', gid: 20, group: 'dialout' }

function stubDevices(list: HostDevice[] | 'fail') {
  const fn = vi.fn().mockImplementation(() =>
    Promise.resolve(
      list === 'fail'
        ? new Response(JSON.stringify({ error: { code: 'internal', message: 'kaputt' } }), { status: 500 })
        : new Response(JSON.stringify(list), { status: 200 }),
    ),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

describe('deviceSnippet', () => {
  it('erzeugt devices und group_add, Gruppen nur einmal', () => {
    expect(deviceSnippet([gpu])).toBe('    devices:\n      - /dev/dri/renderD128:/dev/dri/renderD128\n    group_add:\n      - "991"\n')
    const two = deviceSnippet([gpu, { ...gpu, path: '/dev/dri/renderD129' }, zigbee])
    expect(two.match(/group_add/g)).toHaveLength(1)
    expect(two.match(/"991"/g)).toHaveLength(1)
    expect(two).toContain('"20"')
    expect(deviceSnippet([])).toBe('')
  })
})

describe('Vorlage mit Gerät', () => {
  const jf = APP_TEMPLATES.find((t) => t.id === 'jellyfin')!
  it('ersetzt den Marker durch die Geräte oder entfernt ihn', () => {
    const v = defaultValues(jf, 'jf')
    const without = renderTemplate(jf, 'jf', v)
    expect(without.compose).not.toContain('@devices')
    expect(without.compose).not.toContain('devices:')
    const withGpu = renderTemplate(jf, 'jf', v, [gpu])
    expect(withGpu.compose).not.toContain('@devices')
    expect(withGpu.compose).toContain('    devices:\n      - /dev/dri/renderD128:/dev/dri/renderD128\n    group_add:\n      - "991"\n')
    // Der Block steht im Dienst jellyfin, hinter den volumes.
    expect(withGpu.compose.indexOf('/media:ro')).toBeLessThan(withGpu.compose.indexOf('devices:'))
  })
  it('hat nur bei Jellyfin ein Gerät', () => {
    expect(APP_TEMPLATES.filter((t) => t.device).map((t) => t.id)).toEqual(['jellyfin'])
    for (const t of APP_TEMPLATES) expect(t.compose.includes('# @devices'), t.id).toBe(!!t.device)
  })
})

describe('DevicePicker', () => {
  it('listet Geräte und liefert die gewählten', async () => {
    stubDevices([gpu, zigbee])
    const w = mount(DevicePicker, { props: { visible: true }, global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="device-list"]').text()).toContain('Gruppe render (991)')
    expect(w.find('button[data-test="device-select"]').attributes('disabled')).toBeDefined()
    await w.find('input[data-test="device-/dev/dri/renderD128"]').setValue(true)
    await w.find('button[data-test="device-select"]').trigger('click')
    expect(w.emitted('select')).toEqual([[[gpu]]])
  })
  it('erklärt, wenn nichts gefunden wurde, und zeigt Hinweise (NVIDIA)', async () => {
    stubDevices([])
    const w = mount(DevicePicker, { props: { visible: true }, global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="device-empty"]').text()).toContain('kein passendes Gerät')
    stubDevices([{ ...gpu, note: 'NVIDIA-Karten brauchen das Container Toolkit' }])
    const w2 = mount(DevicePicker, { props: { visible: true }, global: { plugins: plugins() } })
    await flushPromises()
    expect(w2.text()).toContain('Container Toolkit')
  })
})

describe('Geräte in TemplateDialog und StackDialog', () => {
  it('Jellyfin: GPU wählbar, Standard ist „nicht durchreichen“', async () => {
    stubDevices([gpu, zigbee])
    const w = mount(TemplateDialog, { props: { visible: true, existing: [] }, global: { plugins: plugins() } })
    await flushPromises()
    await w.find('[data-test="template-jellyfin"]').trigger('click')
    await flushPromises()
    const sel = w.find('select[data-test="tpl-device-select"]')
    expect((sel.element as HTMLSelectElement).value).toBe('')
    // Nur Geräte der passenden Art (GPU), kein Zigbee-Stick.
    expect(sel.findAll('option').map((o) => o.text())).toEqual(['Nicht durchreichen', 'Intel HD Graphics (/dev/dri/renderD128)'])
    await sel.setValue('/dev/dri/renderD128')
    await w.find('button[data-test="template-next"]').trigger('click')
    const [files] = w.emitted('create')![0] as [{ compose: string }]
    expect(files.compose).toContain('- /dev/dri/renderD128:/dev/dri/renderD128')
  })
  it('Vorlage ohne Gerät zeigt keine Auswahl; ohne Auswahl bleibt compose frei von devices', async () => {
    stubDevices([gpu])
    const w = mount(TemplateDialog, { props: { visible: true, existing: [] }, global: { plugins: plugins() } })
    await flushPromises()
    await w.find('[data-test="template-vaultwarden"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="tpl-device"]').exists()).toBe(false)
    w.unmount()
    const w2 = mount(TemplateDialog, { props: { visible: true, existing: [] }, global: { plugins: plugins() } })
    await flushPromises()
    await w2.find('[data-test="template-jellyfin"]').trigger('click')
    await flushPromises()
    await w2.find('button[data-test="template-next"]').trigger('click')
    const [files] = w2.emitted('create')![0] as [{ compose: string }]
    expect(files.compose).not.toContain('devices:')
  })
  it('fügt Geräte in die compose.yaml ein', async () => {
    stubDevices([gpu])
    const w = mount(StackDialog, { props: { visible: true, stack: { name: 'a', compose: 'services: {}\n', env: '' }, existing: [] }, global: { plugins: plugins() } })
    await flushPromises()
    await w.find('button[data-test="stack-insert-device"]').trigger('click')
    await flushPromises()
    await w.find('input[data-test="device-/dev/dri/renderD128"]').setValue(true)
    await w.find('button[data-test="device-select"]').trigger('click')
    await flushPromises()
    const text = (w.find('textarea[data-test="compose"]').element as HTMLTextAreaElement).value
    expect(text).toContain('devices:\n      - /dev/dri/renderD128:/dev/dri/renderD128')
    expect(w.find('[data-test="stack-device-hint"]').text()).toContain('group_add')
  })
})
