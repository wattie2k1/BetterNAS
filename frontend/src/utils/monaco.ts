// Monaco wird nur für den Compose-Editor gebraucht und deshalb erst beim Öffnen geladen (eigener Chunk).
// monaco-yaml prüft die compose.yaml gegen das mitgelieferte Compose-Spec-Schema (offline, ohne Abruf zur Laufzeit).
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'
import { configureMonacoYaml } from 'monaco-yaml'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import YamlWorker from 'monaco-yaml/yaml.worker?worker'
import composeSchema from '@/assets/compose-spec.json'

const g = self as unknown as { MonacoEnvironment?: { getWorker: (id: string, label: string) => Worker } }
g.MonacoEnvironment = { getWorker: (_id, label) => (label === 'yaml' ? new YamlWorker() : new EditorWorker()) }

/** Modell-URI des Compose-Editors; das Schema gilt nur dafür. */
export const COMPOSE_URI = 'inmemory://betternas/compose.yaml'

const { $schema: _ignored, ...schema } = composeSchema as Record<string, unknown>
void _ignored

configureMonacoYaml(monaco, {
  validate: true,
  enableSchemaRequest: false,
  completion: true,
  hover: true,
  schemas: [{ uri: 'https://raw.githubusercontent.com/compose-spec/compose-spec/main/schema/compose-spec.json', fileMatch: [COMPOSE_URI], schema }],
})

export { monaco }
