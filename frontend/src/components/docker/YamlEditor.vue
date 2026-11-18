<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { editor } from 'monaco-editor'

const model = defineModel<string>({ required: true })
const host = ref<HTMLDivElement>()
let ed: editor.IStandaloneCodeEditor | undefined
let mq: MediaQueryList | undefined

const theme = () => (mq?.matches ? 'vs-dark' : 'vs')
const onScheme = () => ed && (ed.updateOptions({ theme: theme() }))

onMounted(async () => {
  const { monaco, COMPOSE_URI } = await import('@/utils/monaco')
  if (!host.value) return
  mq = window.matchMedia?.('(prefers-color-scheme: dark)')
  const uri = monaco.Uri.parse(COMPOSE_URI)
  const tm = monaco.editor.getModel(uri) ?? monaco.editor.createModel(model.value, 'yaml', uri)
  tm.setValue(model.value)
  ed = monaco.editor.create(host.value, {
    model: tm,
    theme: theme(),
    automaticLayout: true,
    minimap: { enabled: false },
    tabSize: 2,
    insertSpaces: true,
    scrollBeyondLastLine: false,
    fontSize: 13,
    ariaLabel: 'compose.yaml',
  })
  ed.onDidChangeModelContent(() => {
    const v = ed!.getValue()
    if (v !== model.value) model.value = v
  })
  mq?.addEventListener?.('change', onScheme)
})

watch(model, (v) => {
  if (ed && ed.getValue() !== v) ed.setValue(v)
})

onBeforeUnmount(() => {
  mq?.removeEventListener?.('change', onScheme)
  ed?.dispose()
})
</script>

<template>
  <div ref="host" class="yaml-editor" data-test="yaml-editor" />
</template>

<style scoped>
.yaml-editor {
  height: 360px;
  border: 1px solid var(--p-content-border-color);
  border-radius: var(--bn-radius-xs);
  overflow: hidden;
}
</style>
