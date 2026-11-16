// Monaco wird nur für den Compose-Editor gebraucht und deshalb erst beim Öffnen geladen (eigener Chunk).
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'

const g = self as unknown as { MonacoEnvironment?: { getWorker: () => Worker } }
g.MonacoEnvironment = { getWorker: () => new EditorWorker() }

export { monaco }
