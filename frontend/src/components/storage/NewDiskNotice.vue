<script setup lang="ts">
import { watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useStorageStore } from '@/stores/storage'
import { formatBytes } from '@/utils/format'

// Meldet eine neu angeschlossene Platte einmalig per Hinweis, egal auf welcher Seite man gerade ist.
// Platten, die schon beim ersten Laden als „neu“ galten, bekommen keinen Hinweis (der Banner unter „Datenträger“ zeigt sie).
const store = useStorageStore()
const toast = useToast()
const announced = new Set<string>()
let initialised = false

watch(
  () => [store.loaded, store.newDisks.map((d) => d.id).join('|')] as const,
  () => {
    if (!store.loaded) return
    if (!initialised) {
      initialised = true
      store.newDisks.forEach((d) => announced.add(d.id))
      return
    }
    for (const d of store.newDisks) {
      if (announced.has(d.id)) continue
      announced.add(d.id)
      toast.add({
        severity: 'info',
        summary: 'Neue Platte erkannt',
        detail: `${d.model || 'Unbekannt'} (${formatBytes(d.sizeBytes)}, ${d.device}) – unter „Datenträger“ einrichten.`,
        life: 12000,
      })
    }
  },
  { immediate: true },
)
</script>

<template>
  <span hidden data-test="new-disk-notice" />
</template>
