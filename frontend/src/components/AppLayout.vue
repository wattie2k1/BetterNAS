<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Badge from 'primevue/badge'
import Drawer from 'primevue/drawer'
import Toast from 'primevue/toast'
import { useAuthStore } from '@/stores/auth'
import { useJobsStore, type Job } from '@/stores/jobs'
import { useStorageStore } from '@/stores/storage'
import { useUsersStore } from '@/stores/users'
import { useSharesStore } from '@/stores/shares'
import { useDockerStore } from '@/stores/docker'
import { useVmsStore } from '@/stores/vms'
import { useNotificationsStore } from '@/stores/notifications'
import { useBackupStore } from '@/stores/backup'
import { connectEvents } from '@/api/ws'
import JobsPanel from './JobsPanel.vue'

const auth = useAuthStore()
const jobs = useJobsStore()
const storage = useStorageStore()
const usersStore = useUsersStore()
const sharesStore = useSharesStore()
const dockerStore = useDockerStore()
const vmsStore = useVmsStore()
const notificationsStore = useNotificationsStore()
const backupStore = useBackupStore()
const route = useRoute()
const router = useRouter()
const showJobs = ref(false)
const now = ref(new Date())
let clock: ReturnType<typeof setInterval> | undefined
const title = computed(() => (route.meta.title as string) ?? 'BetterNAS')

/** Dock-Einträge; color ist der Verlauf der Symbol-Kachel. */
const nav = [
  { label: 'Datenträger', icon: 'pi pi-database', to: '/speicher/datentraeger', color: ['#0ea5e9', '#22d3ee'] },
  { label: 'Speicherpools', icon: 'pi pi-box', to: '/speicher/pools', color: ['#10b981', '#34d399'] },
  { label: 'Freigaben', icon: 'pi pi-folder-open', to: '/freigaben', color: ['#f59e0b', '#fbbf24'] },
  { label: 'Docker-Stacks', icon: 'pi pi-clone', to: '/docker/stacks', color: ['#2563eb', '#60a5fa'] },
  { label: 'Container', icon: 'pi pi-server', to: '/docker/container', color: ['#0d9488', '#2dd4bf'] },
  { label: 'Docker-Ressourcen', icon: 'pi pi-sliders-h', to: '/docker/ressourcen', color: ['#7c3aed', '#a78bfa'] },
  { label: 'Virtuelle Maschinen', icon: 'pi pi-desktop', to: '/vms', color: ['#f97316', '#fb923c'] },
  { label: 'Benutzer & Gruppen', icon: 'pi pi-users', to: '/benutzer', color: ['#ec4899', '#f472b6'] },
  { label: 'Einstellungen', icon: 'pi pi-cog', to: '/einstellungen', color: ['#64748b', '#94a3b8'] },
]
const gradient = (c: string[]) => `linear-gradient(135deg, ${c[0]}, ${c[1]})`
const initial = computed(() => (auth.user ?? '?').slice(0, 1).toUpperCase())
const time = computed(() => now.value.toLocaleTimeString('de-DE', { hour: '2-digit', minute: '2-digit' }))
const date = computed(() => now.value.toLocaleDateString('de-DE', { weekday: 'long', day: 'numeric', month: 'long' }))

let disconnect: (() => void) | undefined
onMounted(() => {
  clock = setInterval(() => (now.value = new Date()), 15000)
  jobs.load().catch(() => {})
  disconnect = connectEvents((m) => {
    if (m.type === 'job.updated') jobs.update(m.data as Job)
    storage.handleEvent(m)
    usersStore.handleEvent(m)
    sharesStore.handleEvent(m)
    dockerStore.handleEvent(m)
    vmsStore.handleEvent(m)
    notificationsStore.handleEvent(m)
    backupStore.handleEvent(m)
  })
})
onUnmounted(() => {
  disconnect?.()
  clearInterval(clock)
})

async function logout() {
  await auth.logout()
  router.replace({ name: 'login' })
}
</script>

<template>
  <div class="shell">
    <nav class="dock glass" aria-label="Hauptnavigation">
      <RouterLink to="/" class="item home" aria-label="Übersicht" active-class="" exact-active-class="active">
        <span class="logo"><i class="pi pi-server" /></span>
        <span class="tip">Übersicht</span>
      </RouterLink>
      <span class="sep" />
      <RouterLink v-for="item in nav" :key="item.to" :to="item.to" class="item" :aria-label="item.label" active-class="active" exact-active-class="active">
        <span class="icon" :style="{ background: gradient(item.color) }"><i :class="item.icon" /></span>
        <span class="tip">{{ item.label }}</span>
      </RouterLink>
    </nav>
    <div class="main">
      <header class="topbar">
        <div>
          <h1>{{ title }}</h1>
          <div class="date">{{ date }}</div>
        </div>
        <span class="spacer" />
        <div class="chip clock glass">{{ time }}</div>
        <button class="chip round glass" aria-label="Aufträge" @click="showJobs = true">
          <i class="pi pi-list-check" />
          <Badge v-if="jobs.running.length" :value="jobs.running.length" severity="info" class="badge" />
        </button>
        <div class="chip user glass">
          <RouterLink to="/profil" class="profile-link" aria-label="Profil" title="Profil">
            <span class="avatar">{{ initial }}</span>
            <span class="name">{{ auth.user }}</span>
          </RouterLink>
          <button class="logout" aria-label="Abmelden" title="Abmelden" @click="logout"><i class="pi pi-sign-out" /></button>
        </div>
      </header>
      <main class="content">
        <RouterView />
      </main>
    </div>
    <Toast position="bottom-right" />
    <Drawer v-model:visible="showJobs" header="Aufträge" position="right" class="jobs-drawer">
      <JobsPanel />
    </Drawer>
  </div>
</template>

<style scoped>
.shell {
  min-height: 100vh;
  padding-left: 108px;
}
.dock {
  position: fixed;
  left: 18px;
  top: 22px;
  /* kein overflow: sonst werden die Tooltips abgeschnitten und die Leiste scrollt horizontal */
  z-index: 50;
  width: 74px;
  padding: 14px 0;
  border-radius: 28px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}
.logo {
  width: 46px;
  height: 46px;
  border-radius: 16px;
  display: grid;
  place-items: center;
  color: #fff;
  font-size: 1.35rem;
  background: linear-gradient(135deg, var(--bn-accent-1), var(--bn-accent-3));
  box-shadow: 0 8px 22px -6px rgb(139 92 246 / 70%);
}
.sep {
  width: 30px;
  height: 1px;
  background: var(--bn-border-soft);
}
.item {
  position: relative;
  display: grid;
  place-items: center;
  width: 54px;
  height: 54px;
  border-radius: 18px;
  text-decoration: none;
  transition: background 0.15s ease, transform 0.15s ease;
}
.item:hover {
  background: var(--bn-tile-hover);
  transform: translateX(2px) scale(1.06);
}
.icon {
  width: 40px;
  height: 40px;
  border-radius: 13px;
  display: grid;
  place-items: center;
  color: #fff;
  font-size: 1.1rem;
  box-shadow: 0 6px 16px -6px rgb(0 0 0 / 45%);
}
.item.active {
  background: var(--bn-tile-hover);
  box-shadow: 0 0 0 2px var(--bn-accent-1) inset;
}
.tip {
  position: absolute;
  left: 66px;
  white-space: nowrap;
  padding: 0.35rem 0.7rem;
  border-radius: 10px;
  font-size: 0.8rem;
  font-weight: 600;
  color: var(--p-text-color);
  background: var(--bn-glass-strong);
  border: 1px solid var(--bn-border);
  box-shadow: var(--bn-shadow);
  opacity: 0;
  pointer-events: none;
  transform: translateX(-4px);
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.item:hover .tip,
.item:focus-visible .tip {
  opacity: 1;
  transform: none;
}
.main {
  min-width: 0;
  padding: 22px 28px 40px 8px;
  max-width: 1500px;
}
.topbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 22px;
}
.topbar h1 {
  margin: 0;
  font-size: 1.9rem;
  font-weight: 750;
  letter-spacing: -0.03em;
}
.date {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
  margin-top: 2px;
}
.spacer {
  flex: 1;
}
.chip {
  height: 44px;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 0 16px;
  color: var(--p-text-color);
  font: inherit;
  font-weight: 600;
}
.chip.round {
  width: 44px;
  padding: 0;
  justify-content: center;
  cursor: pointer;
  position: relative;
}
.chip.round:hover {
  background: var(--bn-tile-hover);
}
.badge {
  position: absolute;
  top: -4px;
  right: -4px;
}
.user {
  padding: 0 6px 0 5px;
}
.avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: 0.85rem;
  color: #fff;
  background: linear-gradient(135deg, var(--bn-accent-1), var(--bn-accent-3));
}
.profile-link {
  display: flex;
  align-items: center;
  gap: inherit;
  color: inherit;
  text-decoration: none;
}
.logout {
  all: unset;
  cursor: pointer;
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  color: var(--bn-text-soft);
}
.logout:hover {
  background: var(--bn-tile-hover);
  color: var(--p-text-color);
}
/* Niedrige Fenster: Dock verdichten statt scrollen */
@media (min-width: 801px) and (max-height: 760px) {
  .dock {
    top: 10px;
    padding: 8px 0;
    gap: 4px;
  }
  .item {
    width: 46px;
    height: 46px;
  }
  .icon,
  .logo {
    width: 34px;
    height: 34px;
    font-size: 1rem;
  }
  .tip {
    left: 58px;
  }
}
@media (max-width: 800px) {
  .shell {
    padding-left: 0;
    padding-bottom: 96px;
  }
  .dock {
    top: auto;
    bottom: 12px;
    left: 12px;
    right: 12px;
    width: auto;
    transform: none;
    flex-direction: row;
    justify-content: space-between;
    padding: 8px 10px;
    border-radius: 26px;
    gap: 2px;
    overflow-x: auto;
  }
  .sep,
  .tip {
    display: none;
  }
  .item {
    width: 46px;
    height: 46px;
    flex: 0 0 auto;
  }
  .icon,
  .logo {
    width: 36px;
    height: 36px;
    font-size: 1.1rem;
  }
  .main {
    padding: 16px;
  }
  .topbar h1 {
    font-size: 1.5rem;
  }
  .name,
  .clock {
    display: none;
  }
}
</style>
