<script setup lang="ts">
import { ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import ToggleSwitch from 'primevue/toggleswitch'
import type { AutoUpdateConfig } from '@/api/docker'

const props = defineProps<{ visible: boolean; config: AutoUpdateConfig; stacks: string[]; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; save: [AutoUpdateConfig]; run: [] }>()

const enabled = ref(false)
const time = ref('04:00')
const chosen = ref<string[]>([])

watch(
  () => [props.visible, props.config] as const,
  ([v]) => {
    if (!v) return
    enabled.value = props.config.enabled
    time.value = props.config.time
    // Gelöschte Stacks nicht mitschleppen.
    chosen.value = props.config.stacks.filter((s) => props.stacks.includes(s))
  },
  { immediate: true },
)

const valid = () => /^([01]\d|2[0-3]):[0-5]\d$/.test(time.value)
const toggle = (name: string) => {
  chosen.value = chosen.value.includes(name) ? chosen.value.filter((s) => s !== name) : [...chosen.value, name]
}
const save = () => emit('save', { enabled: enabled.value, time: time.value, stacks: chosen.value })
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    header="Automatische Updates"
    :style="{ width: 'min(560px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <p class="hint">
      Die ausgewählten Stacks werden zur eingestellten Uhrzeit aktualisiert, wenn es neue Images gibt. Danach prüft BetterNAS bis zu 10 Minuten lang, ob alle Container
      laufen und gesund sind. Ist das nicht der Fall, wird automatisch auf die vorherige Version zurückgerollt und du wirst benachrichtigt (sofern Benachrichtigungen eingerichtet sind).
      Daten in Volumes und Ordnern werden dabei nicht gesichert.
    </p>
    <div class="row">
      <label for="au-on">Automatische Updates</label>
      <ToggleSwitch v-model="enabled" input-id="au-on" data-test="au-enabled" />
    </div>
    <div class="row">
      <label for="au-time">Uhrzeit</label>
      <input id="au-time" v-model="time" type="time" data-test="au-time" />
    </div>
    <div class="stacks" data-test="au-stacks">
      <p v-if="!stacks.length" class="hint">Es gibt noch keine Stacks.</p>
      <label v-for="s in stacks" :key="s" class="stack">
        <input type="checkbox" :checked="chosen.includes(s)" :data-test="`au-stack-${s}`" @change="toggle(s)" />
        {{ s }}
      </label>
    </div>
    <template #footer>
      <Button
        data-test="au-run"
        label="Jetzt ausführen"
        icon="pi pi-play"
        severity="secondary"
        text
        :disabled="busy || !config.stacks.length"
        :title="config.stacks.length ? 'Prüft sofort auf Updates und aktualisiert die gespeicherten Stacks' : 'Erst Stacks auswählen und speichern'"
        @click="emit('run')"
      />
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="au-save" label="Speichern" icon="pi pi-check" :disabled="busy || !valid()" :loading="busy" @click="save" />
    </template>
  </Dialog>
</template>

<style scoped>
.hint {
  color: var(--bn-text-soft);
  font-size: 0.9rem;
  margin: 0 0 1rem;
}
.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 0.75rem;
}
.stacks {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  margin-top: 1rem;
}
.stack {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
</style>
