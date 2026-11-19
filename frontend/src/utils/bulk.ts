/** Ergebnis einer Sammelaktion: erfolgreiche IDs und Fehler je ID. */
export interface BulkResult {
  done: string[]
  failed: { id: string; message: string }[]
}

/** Führt fn für alle IDs aus (parallel); ein Fehler bricht die übrigen nicht ab. */
export async function runBulk(ids: string[], fn: (id: string) => Promise<unknown>): Promise<BulkResult> {
  const results = await Promise.allSettled(ids.map((id) => fn(id)))
  const out: BulkResult = { done: [], failed: [] }
  results.forEach((r, i) => {
    if (r.status === 'fulfilled') out.done.push(ids[i])
    else out.failed.push({ id: ids[i], message: r.reason instanceof Error ? r.reason.message : String(r.reason) })
  })
  return out
}
