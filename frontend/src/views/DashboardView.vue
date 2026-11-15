<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import Card from 'primevue/card'
import ProgressBar from 'primevue/progressbar'
import Message from 'primevue/message'
import Skeleton from 'primevue/skeleton'
import { api } from '@/api/client'
import { formatBytes, formatUptime } from '@/utils/format'

interface SystemInfo {
  hostname: string
  kernel: string
  os: string
  uptimeSeconds: number
  cpuCount: number
  memTotalBytes: number
  memAvailableBytes: number
  version: string
}

const info = ref<SystemInfo | null>(null)
const error = ref('')
let timer: ReturnType<typeof setInterval> | undefined

async function load() {
  try {
    info.value = await api.get<SystemInfo>('/api/v1/system/info')
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

onMounted(() => {
  load()
  timer = setInterval(load, 10000)
})
onUnmounted(() => clearInterval(timer))

const memUsed = computed(() => (info.value ? info.value.memTotalBytes - info.value.memAvailableBytes : 0))
const memPercent = computed(() => (info.value ? Math.round((memUsed.value / info.value.memTotalBytes) * 100) : 0))
</script>

<template>
  <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
  <div class="grid">
    <Card>
      <template #title>System</template>
      <template #content>
        <dl v-if="info">
          <dt>Gerätename</dt>
          <dd>{{ info.hostname }}</dd>
          <dt>Betriebssystem</dt>
          <dd>{{ info.os }}</dd>
          <dt>Kernel</dt>
          <dd>{{ info.kernel }}</dd>
          <dt>Laufzeit</dt>
          <dd>{{ formatUptime(info.uptimeSeconds) }}</dd>
          <dt>BetterNAS</dt>
          <dd>{{ info.version }}</dd>
        </dl>
        <Skeleton v-else height="8rem" />
      </template>
    </Card>
    <Card>
      <template #title>Arbeitsspeicher</template>
      <template #content>
        <template v-if="info">
          <ProgressBar :value="memPercent" />
          <p class="sub">{{ formatBytes(memUsed) }} von {{ formatBytes(info.memTotalBytes) }} belegt</p>
          <p class="sub">{{ info.cpuCount }} CPU-Kerne</p>
        </template>
        <Skeleton v-else height="4rem" />
      </template>
    </Card>
  </div>
</template>

<style scoped>
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 1rem;
}
dl {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.4rem 1rem;
  margin: 0;
}
dt {
  color: var(--p-text-muted-color);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.sub {
  color: var(--p-text-muted-color);
  margin: 0.6rem 0 0;
}
</style>
