<script setup lang="ts">
import { ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import { useToast } from 'primevue/usetoast'
import { MAX_ICON_BYTES } from '@/api/docker'
import { vmsApi } from '@/api/vms'
import { useVmsStore } from '@/stores/vms'

const props = defineProps<{ vm: string }>()
const open = defineModel<boolean>('visible', { required: true })
const store = useVmsStore()
const toast = useToast()

const fileInput = ref<HTMLInputElement | null>(null)
const urlInput = ref('')
const error = ref('')
const busy = ref(false)
const broken = ref(false)

watch(open, (v) => {
  if (!v) return
  urlInput.value = ''
  error.value = ''
  broken.value = false
})

async function run(fn: () => Promise<void>, done: string) {
  busy.value = true
  error.value = ''
  try {
    await fn()
    await store.refresh()
    broken.value = false
    toast.add({ severity: 'success', summary: done, detail: props.vm, life: 3000 })
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function pickFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  error.value = ''
  if (!f) return
  if (!f.type.startsWith('image/')) {
    error.value = 'Bitte eine Bilddatei wählen (PNG, JPEG, GIF, WebP oder SVG).'
    return
  }
  if (f.size > MAX_ICON_BYTES) {
    error.value = 'Das Bild ist zu groß (höchstens 512 KiB).'
    return
  }
  await run(() => vmsApi.setIconFile(props.vm, f), 'Icon gespeichert')
}

async function applyUrl() {
  const u = urlInput.value.trim()
  if (!/^https?:\/\/\S+$/i.test(u)) {
    error.value = 'Bitte eine Adresse mit http:// oder https:// angeben.'
    return
  }
  await run(() => vmsApi.setIconUrl(props.vm, u), 'Icon gespeichert')
  if (!error.value) urlInput.value = ''
}
</script>

<template>
  <Dialog v-model:visible="open" modal :header="`Icon für ${vm}`" :style="{ width: '30rem', maxWidth: '95vw' }" data-test="vm-icon-dialog">
    <div class="icon-row">
      <span class="icon-preview" :class="{ empty: !store.iconUrlOf(vm) || broken }">
        <img v-if="store.iconUrlOf(vm) && !broken" :src="store.iconUrlOf(vm)!" alt="Aktuelles Icon" data-test="vm-icon-preview" @error="broken = true" />
        <i v-else class="pi pi-desktop" />
      </span>
      <div class="icon-actions">
        <input ref="fileInput" type="file" accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml" hidden data-test="vm-icon-file" @change="pickFile" />
        <Button label="Bild hochladen …" icon="pi pi-upload" size="small" severity="secondary" :disabled="busy" @click="fileInput?.click()" />
        <Button v-if="store.iconUrlOf(vm)" label="Entfernen" icon="pi pi-trash" size="small" text severity="danger" :disabled="busy" data-test="vm-icon-remove" @click="run(() => vmsApi.removeIcon(vm), 'Icon entfernt')" />
      </div>
    </div>
    <div class="field">
      <label for="vm-icon-url">oder Adresse eines Bildes</label>
      <div class="url-row">
        <InputText id="vm-icon-url" v-model="urlInput" placeholder="https://…/icon.png" autocomplete="off" fluid data-test="vm-icon-url" @keydown.enter.prevent="applyUrl" />
        <Button label="Übernehmen" size="small" severity="secondary" :disabled="busy || !urlInput.trim()" data-test="vm-icon-url-apply" @click="applyUrl" />
      </div>
    </div>
    <small v-if="error" class="err" data-test="vm-icon-error">{{ error }}</small>
    <small v-else class="hint">Das Icon erscheint auf der Kachel der VM. PNG, JPEG, GIF, WebP oder SVG, höchstens 512 KiB.</small>
    <template #footer>
      <Button label="Schließen" text @click="open = false" />
    </template>
  </Dialog>
</template>

<style scoped>
.icon-row {
  display: flex;
  align-items: center;
  gap: 1rem;
  margin-bottom: 1rem;
}
.icon-preview {
  width: 72px;
  height: 72px;
  flex: none;
  display: grid;
  place-items: center;
  border-radius: var(--bn-radius-sm);
  background: var(--bn-glass-strong);
  border: 1px solid var(--bn-border);
  overflow: hidden;
}
.icon-preview img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  padding: 8px;
}
.icon-preview.empty {
  color: var(--bn-text-soft);
  font-size: 1.6rem;
}
.icon-actions {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  margin-bottom: 0.5rem;
}
.url-row {
  display: flex;
  gap: 0.5rem;
}
.err {
  color: var(--bn-danger);
}
.hint {
  color: var(--bn-text-soft);
}
</style>
