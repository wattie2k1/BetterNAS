import { computed, ref, watch, type Ref } from 'vue'
import { vmsApi, type VmSnapshot } from '@/api/vms'

/**
 * Lädt beim Öffnen eines Dialogs die Snapshots der VM. Interne Snapshots verhindern Änderungen an den
 * Festplatten; sie dürfen nur nach ausdrücklicher Bestätigung gelöscht werden.
 */
export function useSnapshotGuard(vm: () => string, open: Ref<boolean>) {
  const snapshots = ref<VmSnapshot[]>([])
  const confirmDelete = ref(false)
  const needsConfirm = computed(() => snapshots.value.length > 0)
  /** true, wenn keine Snapshots im Weg sind oder ihr Löschen bestätigt wurde. */
  const ok = computed(() => !needsConfirm.value || confirmDelete.value)
  const deleteSnapshots = computed(() => needsConfirm.value && confirmDelete.value)

  watch(open, async (v) => {
    if (!v) return
    confirmDelete.value = false
    snapshots.value = []
    try {
      snapshots.value = await vmsApi.snapshots(vm())
    } catch {
      /* Fehlt die Bestätigung, meldet der Agent das beim Speichern ohnehin als Fehler. */
    }
  })
  return { snapshots, confirmDelete, needsConfirm, ok, deleteSnapshots }
}
