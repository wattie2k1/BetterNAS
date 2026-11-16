import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import SelectButton from 'primevue/selectbutton'
import Select from 'primevue/select'
import InputNumber from 'primevue/inputnumber'
import VmWizard from '@/components/vms/VmWizard.vue'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))

function route(createStatus = 201) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/isos') return res(['debian.iso', 'virtio-win.iso'])
    if (key === 'GET /api/v1/vms/networks') return res(['default'])
    if (key === 'GET /api/v1/vms/status') return res({ installed: true, active: true, kvm: true })
    if (key === 'GET /api/v1/vms') return res([])
    if (key === 'POST /api/v1/vms')
      return createStatus === 201 ? res({ name: 'x' }, 201) : res({ error: { code: 'conflict', message: 'Eine VM mit diesem Namen existiert bereits' } }, 409)
    return res({ error: { code: 'not_found', message: 'x' } }, 404)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

const mountWizard = () => mount(VmWizard, { props: { visible: true }, global: { plugins: [createPinia(), PrimeVue, ToastService] } })
const next = (w: ReturnType<typeof mountWizard>) => w.find('button[data-test="vm-next"]')
const created = (fn: ReturnType<typeof vi.fn>) => {
  const c = fn.mock.calls.find((c) => c[1]?.method === 'POST' && c[0] === '/api/v1/vms')
  return c ? JSON.parse(c[1].body) : null
}

afterEach(() => vi.unstubAllGlobals())

describe('VmWizard', () => {
  it('sperrt „Weiter“ bei ungültigem Namen', async () => {
    route()
    const w = mountWizard()
    await flushPromises()
    expect(next(w).attributes('disabled')).toBeDefined()
    await w.find('input#vm-name').setValue('Mein Server')
    expect(w.text()).toContain('Nur Kleinbuchstaben')
    expect(next(w).attributes('disabled')).toBeDefined()
    await w.find('input#vm-name').setValue('debian')
    expect(next(w).attributes('disabled')).toBeUndefined()
  })

  it('sendet die erwartete Spec für Linux', async () => {
    const fn = route()
    const w = mountWizard()
    await flushPromises()
    await w.find('input#vm-name').setValue('debian')
    await next(w).trigger('click')
    await next(w).trigger('click')
    ;(w.findComponent('[data-test="vm-iso"]') as unknown as VueWrapper).vm.$emit('update:modelValue', 'debian.iso')
    await w.find('button[data-test="vm-create"]').trigger('click')
    await flushPromises()
    expect(created(fn)).toEqual({
      name: 'debian', os: 'linux', firmware: 'uefi', network: 'default', iso: 'debian.iso', driverIso: '',
      vcpus: 2, memoryMiB: 4096, diskGiB: 40, tpm: false, start: true,
    })
    expect(w.emitted('update:visible')).toEqual([[false]])
  })

  it('belegt Windows mit TPM, 80 GiB und Treiber-ISO-Auswahl vor', async () => {
    const fn = route()
    const w = mountWizard()
    await flushPromises()
    expect(w.find('[data-test="vm-tpm-row"]').exists()).toBe(false)
    await w.find('input#vm-name').setValue('win11')
    w.findComponent(SelectButton).vm.$emit('update:modelValue', 'windows')
    await flushPromises()
    expect(w.find('[data-test="vm-tpm-row"]').exists()).toBe(true)
    await next(w).trigger('click')
    expect((w.find('input#vm-disk').element as HTMLInputElement).value).toBe('80')
    await next(w).trigger('click')
    expect(w.findAllComponents(Select).length).toBeGreaterThanOrEqual(3)
    ;(w.findComponent('[data-test="vm-driver"]') as unknown as VueWrapper).vm.$emit('update:modelValue', 'virtio-win.iso')
    await w.find('button[data-test="vm-create"]').trigger('click')
    await flushPromises()
    expect(created(fn)).toMatchObject({ os: 'windows', firmware: 'uefi', tpm: true, diskGiB: 80, driverIso: 'virtio-win.iso' })
  })

  it('zeigt Treiber-ISO bei Linux nicht', async () => {
    route()
    const w = mountWizard()
    await flushPromises()
    await w.find('input#vm-name').setValue('debian')
    await next(w).trigger('click')
    await next(w).trigger('click')
    expect(w.find('[data-test="vm-driver"]').exists()).toBe(false)
  })

  it('blockiert Schritt 2 bei ungültigen Zahlen', async () => {
    route()
    const w = mountWizard()
    await flushPromises()
    await w.find('input#vm-name').setValue('debian')
    await next(w).trigger('click')
    const cpu = w.findAllComponents(InputNumber)[0]
    cpu.vm.$emit('update:modelValue', 0)
    await flushPromises()
    expect(next(w).attributes('disabled')).toBeDefined()
    cpu.vm.$emit('update:modelValue', null)
    await flushPromises()
    expect(next(w).attributes('disabled')).toBeDefined()
    cpu.vm.$emit('update:modelValue', 4)
    await flushPromises()
    expect(next(w).attributes('disabled')).toBeUndefined()
  })

  it('zeigt API-Fehler im Dialog und bleibt offen', async () => {
    route(409)
    const w = mountWizard()
    await flushPromises()
    await w.find('input#vm-name').setValue('debian')
    await next(w).trigger('click')
    await next(w).trigger('click')
    await w.find('button[data-test="vm-create"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="vm-error"]').text()).toContain('existiert bereits')
    expect(w.emitted('update:visible')).toBeUndefined()
  })
})
