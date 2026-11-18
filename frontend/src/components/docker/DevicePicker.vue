<script setup lang="ts">
import { ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'
import { dockerApi, type HostDevice } from '@/api/docker'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; select: [devices: HostDevice[]] }>()

const devices = ref<HostDevice[]>([])
const chosen = ref<string[]>([])
const loading = ref(false)
const error = ref('')

watch(
  () => props.visible,
  async (v) => {
    if (!v) return
    chosen.value = []
    error.value = ''
    loading.value = true
    try {
      devices.value = await dockerApi.devices()
    } catch (e) {
      devices.value = []
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  },
  { immediate: true },
)

const toggle = (path: string) => {
  chosen.value = chosen.value.includes(path) ? chosen.value.filter((p) => p !== path) : [...chosen.value, path]
}
const KIND: Record<string, string> = { gpu: 'Grafikeinheit', serial: 'USB-Seriell-Adapter' }
const pick = () => {
  emit('select', devices.value.filter((d) => chosen.value.includes(d.path)))
  emit('update:visible', false)
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    header="Gerät durchreichen"
    :style="{ width: 'min(560px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <p v-else-if="!loading && !devices.length" class="hint" data-test="device-empty">
      Es wurde kein passendes Gerät gefunden. Erkannt werden Grafikeinheiten (<code>/dev/dri</code>) und USB-Seriell-Adapter wie Zigbee-Sticks (<code>/dev/serial/by-id</code>).
    </p>
    <ul v-else class="devices" data-test="device-list">
      <li v-for="d in devices" :key="d.path">
        <label>
          <input type="checkbox" :checked="chosen.includes(d.path)" :data-test="`device-${d.path}`" @change="toggle(d.path)" />
          <span>
            <strong>{{ d.name }}</strong>
            <small>{{ KIND[d.kind] ?? d.kind }} · {{ d.path }} · Gruppe {{ d.group }} ({{ d.gid }})</small>
            <small v-if="d.note" class="warn">{{ d.note }}</small>
          </span>
        </label>
      </li>
    </ul>
    <small class="hint">
      Eingefügt werden <code>devices:</code> und <code>group_add:</code> für den Dienst, der das Gerät braucht. Das Gerät ist danach für diesen Container voll zugänglich.
    </small>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="device-select" label="Einfügen" icon="pi pi-check" :disabled="!chosen.length" @click="pick" />
    </template>
  </Dialog>
</template>

<style scoped>
.devices {
  list-style: none;
  margin: 0 0 0.75rem;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.devices label {
  display: flex;
  align-items: flex-start;
  gap: 0.6rem;
  cursor: pointer;
}
.devices span {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
}
.devices small,
.hint {
  color: var(--bn-text-soft);
}
.warn {
  color: var(--bn-warn);
}
</style>
