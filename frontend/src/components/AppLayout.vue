<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Button from 'primevue/button'
import Badge from 'primevue/badge'
import Drawer from 'primevue/drawer'
import Toast from 'primevue/toast'
import { useAuthStore } from '@/stores/auth'
import { useJobsStore, type Job } from '@/stores/jobs'
import { useStorageStore } from '@/stores/storage'
import { useUsersStore } from '@/stores/users'
import { useSharesStore } from '@/stores/shares'
import { connectEvents } from '@/api/ws'
import JobsPanel from './JobsPanel.vue'

const auth = useAuthStore()
const jobs = useJobsStore()
const storage = useStorageStore()
const usersStore = useUsersStore()
const sharesStore = useSharesStore()
const route = useRoute()
const router = useRouter()
const showJobs = ref(false)
const navOpen = ref(false)
const title = computed(() => (route.meta.title as string) ?? 'BetterNAS')

const nav = [
  { label: 'Übersicht', icon: 'pi pi-home', to: '/' },
  { heading: 'Speicher' },
  { label: 'Datenträger', icon: 'pi pi-database', to: '/speicher/datentraeger' },
  { label: 'Speicherpools', icon: 'pi pi-box', to: '/speicher/pools' },
  { heading: 'Freigaben' },
  { label: 'Freigaben', icon: 'pi pi-folder-open', to: '/freigaben' },
  { heading: 'Benutzer' },
  { label: 'Benutzer', icon: 'pi pi-users', to: '/benutzer' },
  { label: 'Gruppen', icon: 'pi pi-sitemap', to: '/benutzer/gruppen' },
]

let disconnect: (() => void) | undefined
onMounted(() => {
  jobs.load().catch(() => {})
  disconnect = connectEvents((m) => {
    if (m.type === 'job.updated') jobs.update(m.data as Job)
    storage.handleEvent(m)
    usersStore.handleEvent(m)
    sharesStore.handleEvent(m)
  })
})
onUnmounted(() => disconnect?.())

async function logout() {
  await auth.logout()
  router.replace({ name: 'login' })
}
</script>

<template>
  <div class="shell" :class="{ 'nav-open': navOpen }">
    <aside class="sidebar">
      <div class="brand"><i class="pi pi-server" /> BetterNAS</div>
      <nav>
        <template v-for="(item, i) in nav" :key="i">
          <div v-if="item.heading" class="heading">{{ item.heading }}</div>
          <RouterLink v-else :to="item.to!" class="link" @click="navOpen = false">
            <i :class="item.icon" /> {{ item.label }}
          </RouterLink>
        </template>
      </nav>
    </aside>
    <div class="main">
      <header class="topbar">
        <Button class="menu" icon="pi pi-bars" text rounded aria-label="Menü" @click="navOpen = !navOpen" />
        <h1>{{ title }}</h1>
        <span class="spacer" />
        <Button text rounded aria-label="Aufträge" @click="showJobs = true">
          <i class="pi pi-list-check" />
          <Badge v-if="jobs.running.length" :value="jobs.running.length" severity="info" />
        </Button>
        <span class="user"><i class="pi pi-user" /> {{ auth.user }}</span>
        <Button icon="pi pi-sign-out" text rounded aria-label="Abmelden" @click="logout" />
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
  display: grid;
  grid-template-columns: 240px 1fr;
  min-height: 100vh;
}
.sidebar {
  background: var(--p-surface-0);
  border-right: 1px solid var(--p-content-border-color);
  padding: 1rem 0.75rem;
}
.brand {
  font-weight: 700;
  font-size: 1.15rem;
  padding: 0.5rem 0.75rem 1.25rem;
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.heading {
  margin: 1rem 0.75rem 0.25rem;
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--p-text-muted-color);
}
.link {
  display: flex;
  gap: 0.6rem;
  align-items: center;
  padding: 0.55rem 0.75rem;
  border-radius: 8px;
  color: var(--p-text-color);
  text-decoration: none;
}
.link:hover {
  background: var(--p-surface-100);
}
.link.router-link-exact-active {
  background: var(--p-primary-50);
  color: var(--p-primary-700);
  font-weight: 600;
}
.main {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.topbar {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.5rem 1.5rem;
  border-bottom: 1px solid var(--p-content-border-color);
  background: var(--p-surface-0);
}
.topbar h1 {
  font-size: 1.15rem;
  margin: 0;
}
.spacer {
  flex: 1;
}
.user {
  display: flex;
  gap: 0.35rem;
  align-items: center;
  color: var(--p-text-muted-color);
}
.menu {
  display: none;
}
.content {
  padding: 1.5rem;
}
@media (prefers-color-scheme: dark) {
  .sidebar,
  .topbar {
    background: var(--p-surface-900);
  }
  .link:hover {
    background: var(--p-surface-800);
  }
  .link.router-link-exact-active {
    background: color-mix(in srgb, var(--p-primary-500) 20%, transparent);
    color: var(--p-primary-200);
  }
}
@media (max-width: 800px) {
  .shell {
    grid-template-columns: 1fr;
  }
  .sidebar {
    display: none;
    position: fixed;
    inset: 0 30% 0 0;
    z-index: 10;
  }
  .nav-open .sidebar {
    display: block;
  }
  .menu {
    display: inline-flex;
  }
  .user {
    display: none;
  }
  .content,
  .topbar {
    padding-inline: 16px;
  }
}
</style>
