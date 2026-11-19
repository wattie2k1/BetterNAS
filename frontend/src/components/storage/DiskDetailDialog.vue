<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import TrendChart from './TrendChart.vue'
import StandbyChart from './StandbyChart.vue'
import UsageBar from './UsageBar.vue'
import { SELFTEST_KIND_LABEL, SSD_WEAR_WARN_PERCENT, storageApi, APM_CHOICES, type DiskNote, type DiskView, type HistoryPoint, type PowerDay, type SelfTestKind, type TuningSettings, type TuningState } from '@/api/storage'
import { useStorageStore } from '@/stores/storage'
import { formatBytes } from '@/utils/format'

const props = defineProps<{ visible: boolean; disk: DiskView | null }>()
const emit = defineEmits<{ 'update:visible': [boolean] }>()

const store = useStorageStore()
const toast = useToast()
const days = ref(30)
const RANGES = [
  { days: 7, label: '7 Tage' },
  { days: 30, label: '30 Tage' },
  { days: 90, label: '90 Tage' },
  { days: 365, label: '1 Jahr' },
]
const history = ref<HistoryPoint[]>([])
const historyError = ref('')
const busy = ref(false)

// Aktuelle Daten der Platte aus dem Store, damit der Fortschritt eines Tests live weiterläuft.
const live = computed(() => store.disks.find((d) => d.id === props.disk?.id) ?? props.disk)
const smart = computed(() => live.value?.smart)
const test = computed(() => smart.value?.selfTest)

async function loadHistory() {
  if (!props.visible || !props.disk) return
  historyError.value = ''
  try {
    history.value = await storageApi.history(props.disk.id, days.value)
  } catch (e) {
    history.value = []
    historyError.value = e instanceof Error ? e.message : String(e)
  }
}
const power = ref<PowerDay[]>([])
async function loadPower() {
  if (!props.visible || !props.disk || !live.value?.rotational) {
    power.value = []
    return
  }
  try {
    power.value = (await storageApi.powerStats(props.disk.id, days.value)) ?? []
  } catch {
    power.value = []
  }
}
watch(() => [props.visible, props.disk?.id, days.value], () => Promise.all([loadHistory(), loadPower()]), { immediate: true })

// APM und Schreibcache gibt es nur für Daten-/Paritätsplatten; die aktuellen Werte liest der Server, ohne schlafende Platten zu wecken.
const tunable = computed(() => !!live.value && (live.value.role === 'data' || live.value.role === 'parity') && live.value.state === 'online')
const tuning = ref<TuningState | null>(null)
const tuneForm = ref<TuningSettings>({ apm: 0, writeCache: '' })
const tuneError = ref('')
async function loadTuning() {
  tuning.value = null
  tuneError.value = ''
  if (!props.visible || !props.disk || !tunable.value) return
  try {
    const t = await storageApi.tuning(props.disk.id)
    // Unvollständige Antworten abfangen, damit der Dialog nie an fehlenden Feldern scheitert.
    tuning.value = { saved: { apm: t?.saved?.apm ?? 0, writeCache: t?.saved?.writeCache ?? '' }, current: t?.current ?? {} }
    tuneForm.value = { ...tuning.value.saved }
  } catch (e) {
    tuneError.value = e instanceof Error ? e.message : String(e)
  }
}
watch(() => [props.visible, props.disk?.id], loadTuning, { immediate: true })
const tuneChanged = computed(() => !!tuning.value && JSON.stringify(tuneForm.value) !== JSON.stringify(tuning.value.saved))
const apmLabel = (v?: number) => (v === undefined ? 'unbekannt' : v === 255 ? 'aus' : String(v))

async function saveTuning() {
  busy.value = true
  tuneError.value = ''
  try {
    await store.setTuning(props.disk!.id, { ...tuneForm.value })
    toast.add({ severity: 'success', summary: 'Einstellungen übernommen', life: 4000 })
    await loadTuning()
  } catch (e) {
    tuneError.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

// NVMe-Werte haben keine Attribut-IDs; dann entfallen die Spalten für ID und Normwerte.
const hasIds = computed(() => (smart.value?.attributes ?? []).some((a) => a.id > 0))

// Notizen: Formular mit den gespeicherten Angaben der Platte füllen, sobald der Dialog aufgeht oder die Platte wechselt.
const emptyNote = (): DiskNote => ({ name: '', notes: '', purchased: '', warrantyUntil: '', tempWarnC: 0 })
const form = ref<DiskNote>(emptyNote())
const noteError = ref('')
watch(
  () => [props.visible, props.disk?.id],
  () => {
    form.value = { ...emptyNote(), ...(live.value?.note ?? {}) }
    noteError.value = ''
  },
  { immediate: true },
)
const noteChanged = computed(() => JSON.stringify(form.value) !== JSON.stringify({ ...emptyNote(), ...(live.value?.note ?? {}) }))

async function saveNote() {
  busy.value = true
  noteError.value = ''
  try {
    await store.setNote(props.disk!.id, { ...form.value, tempWarnC: Number(form.value.tempWarnC) || 0 })
    toast.add({ severity: 'success', summary: 'Notizen gespeichert', life: 4000 })
  } catch (e) {
    noteError.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

/** Hinweis zur Garantie: abgelaufen oder noch gültig (Datum als lokaler Kalendertag). */
const warranty = computed(() => {
  const w = live.value?.note?.warrantyUntil
  if (!w) return null
  const end = new Date(`${w}T23:59:59`)
  if (Number.isNaN(end.getTime())) return null
  const text = end.toLocaleDateString('de-DE', { dateStyle: 'medium' })
  return end.getTime() < Date.now() ? { expired: true, text: `Garantie seit ${text} abgelaufen.` } : { expired: false, text: `Garantie läuft bis ${text}.` }
})

const times = computed(() => history.value.map((p) => p.ts))
const temps = computed(() => history.value.map((p) => p.temperatureC))
const sectors = computed(() => history.value.map((p) => p.reallocatedSectors + p.pendingSectors))

async function run(fn: () => Promise<void>, ok: string) {
  busy.value = true
  try {
    await fn()
    toast.add({ severity: 'success', summary: ok, life: 4000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}
const start = (kind: SelfTestKind) => run(() => store.startSelfTest(props.disk!.id, kind), `${SELFTEST_KIND_LABEL[kind]} gestartet`)
const abort = () => run(() => store.abortSelfTest(props.disk!.id), 'Selbsttest abgebrochen')

const resultTag = { passed: { label: 'bestanden', severity: 'success' }, aborted: { label: 'abgebrochen', severity: 'secondary' }, failed: { label: 'fehlgeschlagen', severity: 'danger' } }
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="disk ? `${live?.note?.name || disk.model || disk.id} (${disk.device})` : 'Datenträger'"
    :style="{ width: 'min(640px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <template v-if="live">
      <dl class="facts">
        <dt>Größe</dt>
        <dd>{{ formatBytes(live.sizeBytes) }} · {{ live.rotational ? 'HDD' : 'SSD' }}<template v-if="live.transport"> · {{ live.transport }}</template></dd>
        <dt>Seriennummer</dt>
        <dd>{{ live.serial || '–' }}</dd>
        <template v-if="live.usage">
          <dt>Belegung</dt>
          <dd><UsageBar :usage="live.usage" /></dd>
        </template>
      </dl>

      <h3>Partitionen</h3>
      <div v-if="live.partitions.length" class="table-wrap">
        <table class="plain" data-test="partitions">
          <thead>
            <tr><th>Gerät</th><th>Dateisystem</th><th>Bezeichnung</th><th>Größe</th><th>Eingehängt unter</th></tr>
          </thead>
          <tbody>
            <tr v-for="p in live.partitions" :key="p.device" :data-test="`part-${p.device}`">
              <td>{{ p.device }}</td>
              <td>{{ p.fsType || '–' }}</td>
              <td>{{ p.label || '–' }}</td>
              <td>{{ formatBytes(p.sizeBytes) }}</td>
              <td>{{ p.mountpoints?.length ? p.mountpoints.join(', ') : '–' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="hint" data-test="no-partitions">Keine Partitionen – die Platte ist leer oder nicht partitioniert.</p>

      <h3>Notizen</h3>
      <form class="notes" data-test="note-form" @submit.prevent="saveNote">
        <label>Bezeichnung<input v-model="form.name" maxlength="60" placeholder="z. B. Schacht 3" data-test="note-name" /></label>
        <div class="dates">
          <label>Gekauft am<input v-model="form.purchased" type="date" data-test="note-purchased" /></label>
          <label>Garantie bis<input v-model="form.warrantyUntil" type="date" data-test="note-warranty" /></label>
        </div>
        <label>Temperaturgrenze (°C)<input v-model.number="form.tempWarnC" type="number" min="30" max="80" placeholder="global" data-test="note-temp" /></label>
        <p class="hint">Warnung ab dieser Temperatur; leer oder 0 nutzt die globale Schwelle aus den Benachrichtigungen.</p>
        <p v-if="warranty" class="hint" :class="{ bad: warranty.expired }" data-test="warranty-hint">{{ warranty.text }}</p>
        <label>Notiz<textarea v-model="form.notes" rows="3" maxlength="1000" placeholder="z. B. gebraucht gekauft, Händler, Einbaudatum" data-test="note-notes" /></label>
        <Message v-if="noteError" severity="error" :closable="false" data-test="note-error">{{ noteError }}</Message>
        <Button type="submit" label="Speichern" size="small" :loading="busy" :disabled="!noteChanged" data-test="note-save" />
      </form>

      <template v-if="tunable">
        <h3>Energie und Cache</h3>
        <form class="notes" data-test="tuning-form" @submit.prevent="saveTuning">
          <p v-if="tuning?.current.standby" class="hint" data-test="tuning-standby">Die Platte schläft; ihre aktuellen Werte werden nicht gelesen, damit sie nicht aufwacht.</p>
          <p v-else-if="tuning" class="hint" data-test="tuning-current">
            Aktuell an der Platte: APM {{ apmLabel(tuning.current.apm) }}, Schreibcache
            {{ tuning.current.writeCache === undefined ? 'unbekannt' : tuning.current.writeCache ? 'an' : 'aus' }}.
          </p>
          <label>
            APM (Energieverwaltung)
            <select v-model.number="tuneForm.apm" :disabled="!tuning" data-test="tuning-apm">
              <option v-for="c in APM_CHOICES" :key="c.value" :value="c.value">{{ c.label }}</option>
            </select>
          </label>
          <p class="hint">Niedrige Werte sparen Strom, parken aber die Köpfe oft und können die Platte schneller verschleißen. 128 erlaubt den Ruhezustand.</p>
          <label>
            Schreibcache
            <select v-model="tuneForm.writeCache" :disabled="!tuning" data-test="tuning-cache">
              <option value="">Unverändert</option>
              <option value="on">An</option>
              <option value="off">Aus</option>
            </select>
          </label>
          <p class="hint">Ohne Schreibcache gehen bei Stromausfall weniger Daten verloren, die Platte schreibt aber deutlich langsamer.</p>
          <Message v-if="tuneError" severity="error" :closable="false" data-test="tuning-error">{{ tuneError }}</Message>
          <Button type="submit" label="Übernehmen" size="small" :loading="busy" :disabled="!tuneChanged" data-test="tuning-save" />
        </form>
      </template>

      <template v-if="smart">
      <dl class="facts">
        <dt>Status</dt>
        <dd><Tag :value="smart.passed ? 'OK' : 'Warnung'" :severity="smart.passed ? 'success' : 'danger'" /></dd>
        <dt>Temperatur</dt>
        <dd>{{ smart.temperatureC ? smart.temperatureC + ' °C' : '–' }}</dd>
        <dt>Betriebsstunden</dt>
        <dd>{{ smart.powerOnHours.toLocaleString('de-DE') }} Std.</dd>
        <dt>Defekte Sektoren</dt>
        <dd :class="{ bad: (smart.reallocatedSectors ?? 0) + (smart.pendingSectors ?? 0) > 0 }">
          {{ smart.reallocatedSectors ?? 0 }} neu zugewiesen, {{ smart.pendingSectors ?? 0 }} instabil
        </dd>
      </dl>

      <template v-if="smart.wear">
        <h3>SSD-Verschleiß</h3>
        <div class="wear" :class="{ bad: (smart.wear.percentUsed ?? 0) >= SSD_WEAR_WARN_PERCENT }" data-test="wear">
          <template v-if="smart.wear.percentUsed !== undefined">
            <div class="progress" role="progressbar" :aria-valuenow="smart.wear.percentUsed" aria-valuemin="0" aria-valuemax="100" :aria-label="`${smart.wear.percentUsed} % verbraucht`">
              <div class="bar" :style="{ width: smart.wear.percentUsed + '%' }" />
            </div>
            <p>
              {{ smart.wear.percentUsed }} % der Lebensdauer verbraucht, etwa {{ 100 - smart.wear.percentUsed }} % übrig.
              <span v-if="smart.wear.percentUsed >= SSD_WEAR_WARN_PERCENT">Plane einen Austausch.</span>
            </p>
          </template>
          <p v-if="smart.wear.bytesWritten" class="sub" data-test="wear-written">Insgesamt geschrieben: {{ formatBytes(smart.wear.bytesWritten) }}</p>
          <p v-if="smart.wear.spareLeftPercent !== undefined" class="sub">Reserve-Speicher: {{ smart.wear.spareLeftPercent }} %</p>
        </div>
      </template>

      <h3>Selbsttest</h3>
      <div class="selftest" data-test="selftest">
        <template v-if="test?.running">
          <div class="progress" role="progressbar" :aria-valuenow="100 - (test.remainingPercent ?? 0)" aria-valuemin="0" aria-valuemax="100">
            <div class="bar" :style="{ width: 100 - (test.remainingPercent ?? 0) + '%' }" />
          </div>
          <p data-test="selftest-running">Selbsttest läuft – noch etwa {{ test.remainingPercent ?? 0 }} %. Die Platte bleibt dabei nutzbar, ist aber langsamer.</p>
          <Button label="Abbrechen" severity="secondary" size="small" outlined :loading="busy" data-test="selftest-abort" @click="abort" />
        </template>
        <template v-else>
          <div class="buttons">
            <Button label="Kurztest starten" size="small" outlined :loading="busy" data-test="selftest-short" @click="start('short')" />
            <Button label="Langtest starten" size="small" outlined :loading="busy" data-test="selftest-long" @click="start('long')" />
          </div>
          <p class="hint">
            Der Kurztest dauert wenige Minuten, der Langtest je nach Größe mehrere Stunden und prüft die ganze Oberfläche.
            <template v-if="smart.standby">Die Platte schläft und wird dafür geweckt.</template>
          </p>
          <p v-if="test?.last" class="last" data-test="selftest-last">
            Letzter Test: {{ SELFTEST_KIND_LABEL[test.last.type] }}
            <Tag :value="resultTag[test.last.result].label" :severity="resultTag[test.last.result].severity" />
            <span v-if="test.last.powerOnHours" class="sub">nach {{ test.last.powerOnHours.toLocaleString('de-DE') }} Betriebsstunden</span>
          </p>
          <p v-else class="hint">Bisher wurde kein Selbsttest protokolliert.</p>
        </template>
      </div>

      <details v-if="smart.attributes?.length" class="attrs" data-test="attributes">
        <summary>SMART-Attribute ({{ smart.attributes.length }})</summary>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th v-if="hasIds">ID</th>
                <th>Name</th>
                <th v-if="hasIds">Wert</th>
                <th v-if="hasIds">Schlechtester</th>
                <th v-if="hasIds">Schwelle</th>
                <th>Rohwert</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(a, i) in smart.attributes" :key="a.id || i" :class="{ failing: a.failing }" :data-test="`attr-${a.id || i}`">
                <td v-if="hasIds">{{ a.id }}</td>
                <td>{{ a.name }}<span v-if="a.prefail" class="sub" title="Fällt dieses Attribut unter die Schwelle, droht der Ausfall"> · Vorwarnung</span></td>
                <td v-if="hasIds">{{ a.value }}</td>
                <td v-if="hasIds">{{ a.worst }}</td>
                <td v-if="hasIds">{{ a.threshold }}</td>
                <td>{{ a.raw }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </details>

      <h3>Verlauf</h3>
      <div class="ranges" role="group" aria-label="Zeitraum">
        <Button
          v-for="r in RANGES"
          :key="r.days"
          :label="r.label"
          size="small"
          :severity="days === r.days ? 'primary' : 'secondary'"
          :outlined="days !== r.days"
          :data-test="`range-${r.days}`"
          @click="days = r.days"
        />
      </div>
      <Message v-if="historyError" severity="error" :closable="false">{{ historyError }}</Message>
      <div class="charts">
        <StandbyChart v-if="live.rotational" :days="power" :range="days" />
        <TrendChart title="Temperatur" unit="°C" :values="temps" :times="times" :warn-at="50" />
        <TrendChart title="Defekte Sektoren" :values="sectors" :times="times" :warn-at="1" />
      </div>
      </template>
      <p v-else class="hint" data-test="no-smart">Für diese Platte liegen keine SMART-Werte vor (häufig bei USB-Gehäusen).</p>
    </template>
  </Dialog>
</template>

<style scoped>
.facts {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.4rem 1rem;
  margin: 0 0 1rem;
  align-items: center;
}
dt {
  color: var(--bn-text-soft);
}
dd {
  margin: 0;
}
dd.bad {
  color: var(--bn-warn);
}
h3 {
  margin: 1.2rem 0 0.5rem;
  font-size: 1rem;
}
.buttons,
.ranges {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.hint,
.sub {
  font-size: 0.8rem;
  color: var(--bn-text-soft);
}
.last {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  flex-wrap: wrap;
}
.progress {
  height: 10px;
  border-radius: 99px;
  background: var(--bn-border-soft);
  overflow: hidden;
}
.bar {
  height: 100%;
  background: linear-gradient(90deg, var(--bn-accent-1), var(--bn-accent-2));
  transition: width 0.5s;
}
.wear.bad .bar {
  background: var(--bn-warn);
}
.wear.bad > p:first-of-type {
  color: var(--bn-warn);
}
.attrs {
  margin-top: 1rem;
}
.attrs summary {
  cursor: pointer;
  font-weight: 600;
}
.attrs table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.8rem;
  margin-top: 0.5rem;
}
.attrs th,
.attrs td {
  text-align: left;
  padding: 0.25rem 0.5rem;
  border-bottom: 1px solid var(--bn-border-soft);
}
.attrs tr.failing td {
  color: var(--bn-danger);
}
.plain {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.85rem;
}
.plain th,
.plain td {
  text-align: left;
  padding: 0.3rem 0.5rem;
  border-bottom: 1px solid var(--bn-border-soft);
}
.notes {
  display: grid;
  gap: 0.6rem;
}
.notes label {
  display: grid;
  gap: 0.2rem;
  font-size: 0.85rem;
  color: var(--bn-text-soft);
}
.notes input,
.notes textarea {
  padding: 0.4rem 0.6rem;
  font: inherit;
  color: var(--bn-text);
}
.dates {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.6rem;
}
.hint.bad {
  color: var(--bn-warn);
}
.charts {
  display: grid;
  gap: 1rem;
  margin-top: 0.8rem;
}
</style>
