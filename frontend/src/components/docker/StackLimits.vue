<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import Message from 'primevue/message'
import { api } from '@/api/client'
import { formatBytes } from '@/utils/format'
import { formatMemory, memoryString, parseMemory, readLimits, setLimits } from '@/utils/composeLimits'

const compose = defineModel<string>({ required: true })

interface Row {
  service: string
  memValue: string | number
  memUnit: 'm' | 'g'
  cpus: string | number
  /** Wert steht als Variable o. Ä. in der Datei und lässt sich hier nicht bearbeiten. */
  memCustom: string | null
  cpusCustom: string | null
  error: string
  warn: string
}

const rows = ref<Row[]>([])
const parseError = ref('')
const host = ref<{ cpuCount: number; memTotalBytes: number } | null>(null)
let lastEmitted: string | null = null

onMounted(async () => {
  try {
    host.value = await api.get<{ cpuCount: number; memTotalBytes: number }>('/api/v1/system/info')
  } catch {
    host.value = null // ohne Host-Angaben fehlen nur die Plausibilitätsprüfungen
  }
})

function rebuild(text: string) {
  const r = readLimits(text)
  if (!r.ok) {
    parseError.value = r.error
    rows.value = []
    return
  }
  parseError.value = ''
  rows.value = r.services.map((s) => {
    const row: Row = { service: s.service, memValue: '', memUnit: 'g', cpus: '', memCustom: null, cpusCustom: null, error: '', warn: '' }
    if (s.memory !== null) {
      const bytes = parseMemory(s.memory)
      if (bytes === null) row.memCustom = s.memory
      else Object.assign(row, { memValue: String(formatMemory(bytes).value), memUnit: formatMemory(bytes).unit })
    }
    if (s.cpus !== null) {
      if (/^\d+(\.\d+)?$/.test(s.cpus)) row.cpus = s.cpus
      else row.cpusCustom = s.cpus
    }
    return row
  })
}

// Änderungen von außen (Editor, Vorlage) neu einlesen; die eigenen Änderungen nicht, damit Eingaben nicht springen.
watch(
  compose,
  (v) => {
    if (v !== lastEmitted) rebuild(v)
  },
  { immediate: true },
)

const MIN_MB = 6 // Docker verlangt mindestens 6 MB

function commit(r: Row) {
  r.error = ''
  r.warn = ''
  const patch: { memory?: string | null; cpus?: string | null } = {}
  if (!r.memCustom) {
    const v = String(r.memValue ?? '').trim() // type=number liefert bei v-model Zahlen
    if (v === '') patch.memory = null
    else {
      const n = Number(v)
      if (!Number.isFinite(n) || n <= 0) return void (r.error = 'Arbeitsspeicher: eine Zahl größer als 0 angeben.')
      const mb = r.memUnit === 'g' ? n * 1024 : n
      if (mb < MIN_MB) return void (r.error = `Arbeitsspeicher: mindestens ${MIN_MB} MB.`)
      if (host.value && mb * 1024 ** 2 > host.value.memTotalBytes) r.warn = `Mehr als der Arbeitsspeicher des NAS (${formatBytes(host.value.memTotalBytes)}); das Limit greift nie.`
      patch.memory = memoryString(n, r.memUnit)
    }
  }
  if (!r.cpusCustom) {
    const v = String(r.cpus ?? '').trim().replace(',', '.')
    if (v === '') patch.cpus = null
    else {
      const n = Number(v)
      if (!/^\d+(\.\d+)?$/.test(v) || n <= 0) return void (r.error = 'CPU-Kerne: eine Zahl größer als 0 angeben, z. B. 1.5.')
      if (host.value && n > host.value.cpuCount) return void (r.error = `CPU-Kerne: das NAS hat nur ${host.value.cpuCount} Kerne (Docker startet den Container sonst nicht).`)
      patch.cpus = String(n)
    }
  }
  const next = setLimits(compose.value, r.service, patch)
  lastEmitted = next
  compose.value = next
}

const hostText = computed(() => (host.value ? `Das NAS hat ${host.value.cpuCount} Kerne und ${formatBytes(host.value.memTotalBytes)} Arbeitsspeicher.` : ''))
</script>

<template>
  <div data-test="limits">
    <p class="hint">
      Limits begrenzen, wie viel Arbeitsspeicher und Rechenzeit ein Dienst höchstens nutzt; leer heißt unbegrenzt. Wer sein Arbeitsspeicher-Limit überschreitet, wird von Docker beendet – wähle es daher nicht zu knapp.
      Die Werte stehen danach in der compose.yaml und gelten ab „Speichern und starten“ (der Dienst wird dabei neu erstellt).
    </p>
    <p v-if="hostText" class="hint" data-test="limits-host">{{ hostText }}</p>
    <Message v-if="parseError" severity="warn" :closable="false" data-test="limits-invalid">
      Die compose.yaml ist nicht gültig und kann hier nicht bearbeitet werden: {{ parseError }}
    </Message>
    <p v-else-if="!rows.length" class="hint" data-test="limits-empty">Die compose.yaml enthält keine Dienste.</p>
    <div v-for="r in rows" :key="r.service" class="svc" :data-test="`limits-${r.service}`">
      <strong>{{ r.service }}</strong>
      <div class="fields">
        <label>
          Arbeitsspeicher
          <span v-if="r.memCustom" class="custom">{{ r.memCustom }}</span>
          <span v-else class="pair">
            <input v-model="r.memValue" type="number" min="0" step="any" placeholder="unbegrenzt" :data-test="`limits-${r.service}-mem`" @change="commit(r)" />
            <select v-model="r.memUnit" :data-test="`limits-${r.service}-unit`" @change="commit(r)">
              <option value="m">MB</option>
              <option value="g">GB</option>
            </select>
          </span>
        </label>
        <label>
          CPU-Kerne
          <span v-if="r.cpusCustom" class="custom">{{ r.cpusCustom }}</span>
          <input v-else v-model="r.cpus" type="number" min="0" step="any" placeholder="unbegrenzt" :data-test="`limits-${r.service}-cpus`" @change="commit(r)" />
        </label>
      </div>
      <small v-if="r.memCustom || r.cpusCustom" class="hint">Ein Wert enthält eine Variable oder ein anderes Format; ändere ihn im Reiter compose.yaml.</small>
      <small v-if="r.error" class="err" :data-test="`limits-${r.service}-error`">{{ r.error }}</small>
      <small v-else-if="r.warn" class="warn">{{ r.warn }}</small>
    </div>
  </div>
</template>

<style scoped>
.hint {
  color: var(--bn-text-soft);
  font-size: 0.9rem;
  margin: 0 0 0.75rem;
}
.svc {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  padding: 0.75rem 0;
  border-top: 1px solid var(--p-content-border-color);
}
.fields {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem 1.5rem;
}
.fields label {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  font-size: 0.85rem;
  color: var(--bn-text-soft);
}
.pair {
  display: flex;
  gap: 0.35rem;
}
.pair input,
.fields input {
  width: 9rem;
}
.custom {
  font-family: var(--bn-mono, monospace);
  color: inherit;
}
.err {
  color: var(--bn-danger);
}
.warn {
  color: var(--bn-warn);
}
</style>
