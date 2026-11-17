import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import TemplateDialog from '@/components/docker/TemplateDialog.vue'
import { APP_TEMPLATES, defaultValues, fieldError, randomSecret, renderTemplate } from '@/utils/appTemplates'

describe('Vorlagen', () => {
  it('jede Vorlage hat für alle ${VARIABLEN} ein Feld und gültige Vorgaben', () => {
    for (const t of APP_TEMPLATES) {
      const used = [...t.compose.matchAll(/\$\{([A-Z_]+)\}/g)].map((m) => m[1])
      const keys = t.fields.map((f) => f.key)
      for (const u of used) expect(keys, `${t.id}: ${u}`).toContain(u)
      for (const k of keys) expect(used, `${t.id}: ${k} ungenutzt`).toContain(k)
      const v = defaultValues(t, t.stack)
      for (const f of t.fields) expect(fieldError(f, v[f.key]), `${t.id}.${f.key}`).toBe('')
    }
  })

  it('rendert die .env aus den Werten', () => {
    const t = APP_TEMPLATES.find((x) => x.id === 'jellyfin')!
    const files = renderTemplate(t, 'jf', { PORT: '9000', CONFIG_DIR: '/srv/appdata/jf/config', CACHE_DIR: '/srv/appdata/jf/cache', MEDIA_DIR: '/srv/pools/a' })
    expect(files.name).toBe('jf')
    expect(files.env).toBe('PORT=9000\nCONFIG_DIR=/srv/appdata/jf/config\nCACHE_DIR=/srv/appdata/jf/cache\nMEDIA_DIR=/srv/pools/a\n')
    expect(files.compose).toContain('${PORT}:8096')
  })

  it('prüft Eingaben', () => {
    const port = { key: 'P', label: '', kind: 'port' as const }
    const path = { key: 'D', label: '', kind: 'path' as const }
    const secret = { key: 'S', label: '', kind: 'secret' as const }
    expect(fieldError(port, '0')).not.toBe('')
    expect(fieldError(port, '65536')).not.toBe('')
    expect(fieldError(port, '8080')).toBe('')
    expect(fieldError(path, 'relativ')).not.toBe('')
    expect(fieldError(path, '/srv/../etc')).not.toBe('')
    expect(fieldError(path, '/srv/a b')).not.toBe('')
    expect(fieldError(path, '/srv/pools/x')).toBe('')
    expect(fieldError(secret, 'kurz')).not.toBe('')
    expect(fieldError(secret, randomSecret())).toBe('')
    expect(randomSecret()).not.toBe(randomSecret())
  })
})

describe('TemplateDialog', () => {
  const mountIt = () => mount(TemplateDialog, { props: { visible: true, existing: ['jellyfin'] }, global: { plugins: [PrimeVue] }, attachTo: document.body })

  it('wählt Vorlage, vergibt freien Namen und liefert die Dateien', async () => {
    const w = mountIt()
    await w.vm.$nextTick()
    document.querySelector<HTMLButtonElement>('[data-test="template-jellyfin"]')!.click()
    await w.vm.$nextTick()
    const name = document.querySelector<HTMLInputElement>('#tpl-name')!
    expect(name.value).toBe('jellyfin-2')
    expect(document.querySelector<HTMLInputElement>('[data-test="tpl-field-CONFIG_DIR"]')!.value).toBe('/srv/appdata/jellyfin-2/config')
    document.querySelector<HTMLButtonElement>('[data-test="template-next"]')!.click()
    await w.vm.$nextTick()
    const ev = w.emitted('create')![0][0] as { name: string; env: string }
    expect(ev.name).toBe('jellyfin-2')
    expect(ev.env).toContain('PORT=8096')
    w.unmount()
  })
})
