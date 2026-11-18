<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import ToggleSwitch from 'primevue/toggleswitch'
import { sharesApi } from '@/api/shares'
import { SHARED_TAG_RE, type VmConfig, type VmConfigUpdate, type VmSharedDir } from '@/api/vms'

export interface SharedState {
  dirty: boolean
  valid: boolean
  update: Partial<VmConfigUpdate>
}

const props = defineProps<{ cfg: VmConfig; editable: boolean; reset: number }>()
const emit = defineEmits<{ change: [state: SharedState] }>()

const rows = ref<VmSharedDir[]>([])
const shares = ref<{ name: string; path: string }[]>([])

const clean = (p: string) => p.replace(/\/+$/, '')
const slug = (s: string) => s.replace(/[^A-Za-z0-9_-]/g, '_').replace(/^[^A-Za-z0-9]+/, '').slice(0, 36)

function load() {
  rows.value = (props.cfg.sharedDirs ?? []).map((d) => ({ ...d }))
}
watch(() => [props.cfg, props.reset], load, { immediate: true })
onMounted(async () => {
  try {
    shares.value = (await sharesApi.list()).map((s) => ({ name: s.name, path: clean(s.path) }))
  } catch {
    shares.value = []
  }
})

/** Freigaben des NAS plus bereits gesetzte Ordner, die keine Freigabe (mehr) sind. */
const options = computed(() => {
  const list = shares.value.map((s) => ({ label: `${s.name} – ${s.path}`, value: s.path }))
  for (const r of rows.value) if (r.path && !list.some((o) => o.value === r.path)) list.push({ label: r.path, value: r.path })
  return list
})

function add() {
  const used = new Set(rows.value.map((r) => r.path))
  const first = shares.value.find((s) => !used.has(s.path))
  const tag = first ? slug(first.name) : ''
  rows.value.push({ path: first?.path ?? '', tag, readOnly: false })
}
function pick(r: VmSharedDir, path: string) {
  const old = shares.value.find((s) => s.path === r.path)
  const next = shares.value.find((s) => s.path === path)
  // Den Namen nur mitführen, solange er noch dem Vorschlag der alten Freigabe entspricht.
  if (next && (!r.tag || (old && r.tag === slug(old.name)))) r.tag = slug(next.name)
  r.path = path
}

const tagsOk = computed(() => {
  const tags = rows.value.map((r) => r.tag)
  return rows.value.every((r) => SHARED_TAG_RE.test(r.tag) && r.path) && new Set(tags).size === tags.length
})
const changed = computed(() => {
  const key = (d: VmSharedDir) => `${d.path}|${d.tag}|${d.readOnly}`
  const cur = (props.cfg.sharedDirs ?? []).map(key)
  return cur.length !== rows.value.length || rows.value.some((r, i) => key(r) !== cur[i])
})
const state = computed<SharedState>(() => ({
  dirty: changed.value,
  valid: tagsOk.value && rows.value.length <= 8,
  update: changed.value ? { sharedDirs: rows.value.map((r) => ({ ...r })) } : {},
}))
watch(state, (s) => emit('change', s), { immediate: true })

const sampleTag = computed(() => rows.value[0]?.tag || 'daten')
</script>

<template>
  <section class="shared" data-test="shared">
    <h3>Geteilte Ordner</h3>
    <p class="hint">Ordner vom NAS erscheinen in der VM als eingebundenes Laufwerk (virtiofs), ohne Netzwerkfreigabe. Nur bei ausgeschalteter VM änderbar.</p>
    <div v-for="(r, i) in rows" :key="i" class="row-item" data-test="shared-row">
      <Select :model-value="r.path" :options="options" option-label="label" option-value="value" placeholder="Ordner wählen" :disabled="!editable" data-test="shared-path" @update:model-value="(v: string) => pick(r, v)" />
      <div class="line">
        <InputText v-model="r.tag" class="tag" :invalid="!SHARED_TAG_RE.test(r.tag)" placeholder="Name im Gast" :disabled="!editable" data-test="shared-tag" />
        <label class="ro">
          <ToggleSwitch v-model="r.readOnly" :disabled="!editable" aria-label="Nur lesen" data-test="shared-ro" />
          <span>Nur lesen</span>
        </label>
        <Button icon="pi pi-trash" severity="danger" text :disabled="!editable" aria-label="Ordner entfernen" data-test="shared-remove" @click="rows.splice(i, 1)" />
      </div>
    </div>
    <Button label="Ordner hinzufügen" icon="pi pi-plus" size="small" severity="secondary" :disabled="!editable || rows.length >= 8" data-test="shared-add" @click="add" />
    <small v-if="!tagsOk" class="hint bad" data-test="shared-bad">Jeder Ordner braucht einen eindeutigen Namen aus Buchstaben, Ziffern, - und _ (höchstens 36 Zeichen).</small>
    <template v-if="rows.length">
      <p class="hint warn" data-test="shared-warn">Die VM bekommt Zugriff auf den Ordner mit den Rechten des Gast-Systems – ein Root im Gast kann dort alles lesen und ändern. Für sensible Daten „Nur lesen“ nutzen. Snapshots sind mit geteilten Ordnern nur bei ausgeschalteter VM sinnvoll.</p>
      <details class="how">
        <summary>So bindest du den Ordner im Gast ein</summary>
        <p><strong>Linux:</strong> <code>sudo mount -t virtiofs {{ sampleTag }} /mnt/{{ sampleTag }}</code><br />dauerhaft in <code>/etc/fstab</code>: <code>{{ sampleTag }} /mnt/{{ sampleTag }} virtiofs defaults 0 0</code></p>
        <p><strong>Windows:</strong> WinFsp und den „VirtIO-FS“-Dienst aus den virtio-win-Treibern installieren, der Ordner erscheint danach als Laufwerk.</p>
      </details>
    </template>
  </section>
</template>

<style scoped>
.shared {
  margin-top: 1.25rem;
  border-top: 1px solid var(--bn-border, rgba(128, 128, 128, 0.25));
  padding-top: 0.75rem;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  align-items: flex-start;
}
.shared h3 {
  margin: 0;
  font-size: 0.95rem;
}
.hint {
  margin: 0;
  color: var(--bn-text-soft);
  font-size: 0.8rem;
}
.hint.bad {
  color: var(--bn-danger);
}
.hint.warn {
  color: var(--bn-warn);
}
.row-item {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  width: 100%;
}
.line {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}
.tag {
  flex: 1;
  min-width: 0;
}
.ro {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  white-space: nowrap;
}
.how {
  font-size: 0.85rem;
}
.how summary {
  cursor: pointer;
}
</style>
