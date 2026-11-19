<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import TrendChart from './TrendChart.vue'
import UsageBar from './UsageBar.vue'
import { SELFTEST_KIND_LABEL, SSD_WEAR_WARN_PERCENT, storageApi, type DiskView, type HistoryPoint, type SelfTestKind } from '@/api/storage'
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
watch(() => [props.visible, props.disk?.id, days.value], loadHistory, { immediate: true })

// NVMe-Werte haben keine Attribut-IDs; dann entfallen die Spalten für ID und Normwerte.
const hasIds = computed(() => (smart.value?.attributes ?? []).some((a) => a.id > 0))

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
    :header="disk ? `${disk.model || disk.id} (${disk.device})` : 'Datenträger'"
    :style="{ width: 'min(640px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <template v-if="live && smart">
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
        <template v-if="live.usage">
          <dt>Belegung</dt>
          <dd><UsageBar :usage="live.usage" /></dd>
        </template>
        <dt>Größe</dt>
        <dd>{{ formatBytes(live.sizeBytes) }}</dd>
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
        <TrendChart title="Temperatur" unit="°C" :values="temps" :times="times" :warn-at="50" />
        <TrendChart title="Defekte Sektoren" :values="sectors" :times="times" :warn-at="1" />
      </div>
    </template>
    <p v-else>Für diese Platte liegen keine SMART-Werte vor.</p>
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
.charts {
  display: grid;
  gap: 1rem;
  margin-top: 0.8rem;
}
</style>
