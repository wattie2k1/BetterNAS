import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import StackLimits from '@/components/docker/StackLimits.vue'
import StackDialog from '@/components/docker/StackDialog.vue'

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

function stubHost(info: { cpuCount: number; memTotalBytes: number } | null = { cpuCount: 4, memTotalBytes: 8 * 1024 ** 3 }) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation(() => Promise.resolve(info ? new Response(JSON.stringify(info), { status: 200 }) : new Response('{}', { status: 500 }))),
  )
}

const compose = `services:
  web:
    image: nginx
    mem_limit: 512m
    cpus: 1.5
  db:
    image: postgres
    mem_limit: \${DB_MEM}
`

async function mountLimits(text = compose) {
  const w = mount(StackLimits, { props: { modelValue: text, 'onUpdate:modelValue': (v: string) => w.setProps({ modelValue: v }) }, global: { plugins: plugins() } })
  await flushPromises()
  return w
}
const latest = (w: Awaited<ReturnType<typeof mountLimits>>) => w.props('modelValue') as string

describe('StackLimits', () => {
  it('zeigt vorhandene Limits, Host-Angaben und lässt Variablen in Ruhe', async () => {
    stubHost()
    const w = await mountLimits()
    expect(w.find('[data-test="limits-host"]').text()).toContain('4 Kerne')
    expect((w.find('input[data-test="limits-web-mem"]').element as HTMLInputElement).value).toBe('512')
    expect((w.find('select[data-test="limits-web-unit"]').element as HTMLSelectElement).value).toBe('m')
    expect((w.find('input[data-test="limits-web-cpus"]').element as HTMLInputElement).value).toBe('1.5')
    expect(w.find('[data-test="limits-db"]').text()).toContain('${DB_MEM}')
    expect(w.find('input[data-test="limits-db-mem"]').exists()).toBe(false)
    expect(w.find('input[data-test="limits-db-cpus"]').exists()).toBe(true)
  })

  it('schreibt geänderte Werte in die compose.yaml', async () => {
    stubHost()
    const w = await mountLimits()
    await w.find('input[data-test="limits-web-mem"]').setValue('2')
    await w.find('select[data-test="limits-web-unit"]').setValue('g')
    expect(latest(w)).toContain('mem_limit: 2g')
    await w.find('input[data-test="limits-db-cpus"]').setValue('2')
    expect(latest(w)).toContain('cpus: 2')
    expect(latest(w)).toContain('mem_limit: ${DB_MEM}') // Variable bleibt unberührt
    await w.find('input[data-test="limits-web-cpus"]').setValue('')
    expect(latest(w)).not.toContain('cpus: 1.5')
    await w.find('input[data-test="limits-web-mem"]').setValue('')
    expect(latest(w)).not.toContain('mem_limit: 2g')
  })

  it('lehnt ungültige Werte ab und ändert dann nichts', async () => {
    stubHost()
    const w = await mountLimits()
    const before = latest(w)
    await w.find('input[data-test="limits-web-cpus"]').setValue('8')
    expect(w.find('[data-test="limits-web-error"]').text()).toContain('nur 4 Kerne')
    await w.find('input[data-test="limits-web-mem"]').setValue('3')
    await w.find('select[data-test="limits-web-unit"]').setValue('m')
    expect(w.find('[data-test="limits-web-error"]').text()).toContain('mindestens 6 MB')
    await w.find('input[data-test="limits-web-mem"]').setValue('-5')
    expect(w.find('[data-test="limits-web-error"]').exists()).toBe(true)
    expect(latest(w)).toBe(before)
  })

  it('warnt, wenn das Limit größer als der Arbeitsspeicher des NAS ist', async () => {
    stubHost()
    const w = await mountLimits()
    await w.find('select[data-test="limits-web-unit"]').setValue('g')
    await w.find('input[data-test="limits-web-mem"]').setValue('64')
    expect(w.text()).toContain('Mehr als der Arbeitsspeicher des NAS')
    expect(latest(w)).toContain('mem_limit: 64g')
  })

  it('arbeitet auch ohne Host-Angaben und liest Änderungen von außen neu ein', async () => {
    stubHost(null)
    const w = await mountLimits()
    expect(w.find('[data-test="limits-host"]').exists()).toBe(false)
    await w.find('input[data-test="limits-web-cpus"]').setValue('64')
    expect(latest(w)).toContain('cpus: 64')
    await w.setProps({ modelValue: 'services:\n  neu:\n    image: x\n    mem_limit: 1g\n' })
    expect(w.find('[data-test="limits-web"]').exists()).toBe(false)
    expect((w.find('input[data-test="limits-neu-mem"]').element as HTMLInputElement).value).toBe('1')
    expect((w.find('select[data-test="limits-neu-unit"]').element as HTMLSelectElement).value).toBe('g')
  })

  it('erklärt ungültiges YAML und leere Dateien', async () => {
    stubHost()
    const w = await mountLimits('services: [\n  a')
    expect(w.find('[data-test="limits-invalid"]').exists()).toBe(true)
    await w.setProps({ modelValue: 'name: x\n' })
    expect(w.find('[data-test="limits-empty"]').exists()).toBe(true)
  })
})

describe('Reiter „Ressourcen“ im Stack-Dialog', () => {
  it('ändert die compose.yaml, die beim Speichern gesendet wird', async () => {
    stubHost()
    const w = mount(StackDialog, {
      props: { visible: true, stack: { name: 'a', compose: 'services:\n  web:\n    image: nginx\n', env: '' }, existing: [] },
      global: { plugins: plugins() },
    })
    await flushPromises()
    await w.find('input[data-test="limits-web-mem"]').setValue('256')
    await w.find('select[data-test="limits-web-unit"]').setValue('m')
    await w.find('button[data-test="stack-save"]').trigger('click')
    const [files] = w.emitted('submit')![0] as unknown as [{ compose: string }]
    expect(files.compose).toBe('services:\n  web:\n    image: nginx\n    mem_limit: 256m\n')
  })
})
