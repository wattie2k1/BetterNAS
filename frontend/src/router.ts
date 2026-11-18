import { createRouter, createWebHistory } from 'vue-router'
import { setUnauthorizedHandler } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { SETUP_PATH, useSetupStore } from '@/stores/setup'
import AppLayout from '@/components/AppLayout.vue'
import LoginView from '@/views/LoginView.vue'
import DashboardView from '@/views/DashboardView.vue'
import DisksView from '@/views/storage/DisksView.vue'
import UsersGroupsView from '@/views/users/UsersGroupsView.vue'
import UpdatesView from '@/views/system/UpdatesView.vue'
import NotificationsView from '@/views/system/NotificationsView.vue'
import BackupView from '@/views/system/BackupView.vue'
import SettingsView from '@/views/settings/SettingsView.vue'
import NetworkSettings from '@/views/settings/NetworkSettings.vue'
import SetupView from '@/views/SetupView.vue'
import SharesView from '@/views/shares/SharesView.vue'
import PoolsView from '@/views/storage/PoolsView.vue'
import StacksView from '@/views/docker/StacksView.vue'
import ResourcesView from '@/views/docker/ResourcesView.vue'
import ContainersView from '@/views/docker/ContainersView.vue'
import ProfileView from '@/views/ProfileView.vue'
import VmsView from '@/views/vms/VmsView.vue'
import VmConfigView from '@/views/vms/VmConfigView.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginView, meta: { public: true } },
    {
      path: '/',
      component: AppLayout,
      children: [
        { path: '', name: 'dashboard', component: DashboardView, meta: { title: 'Übersicht' } },
        { path: 'speicher/datentraeger', name: 'disks', component: DisksView, meta: { title: 'Datenträger' } },
        { path: 'speicher/pools', name: 'pools', component: PoolsView, meta: { title: 'Speicherpools' } },
        { path: SETUP_PATH.slice(1), name: 'setup', component: SetupView, meta: { title: 'Einrichtung' } },
        { path: 'freigaben', name: 'shares', component: SharesView, meta: { title: 'Freigaben' } },
        { path: 'docker/stacks', name: 'stacks', component: StacksView, meta: { title: 'Docker-Stacks' } },
        { path: 'docker/container', name: 'containers', component: ContainersView, meta: { title: 'Container' } },
        { path: 'docker/ressourcen', name: 'resources', component: ResourcesView, meta: { title: 'Docker-Ressourcen' } },
        { path: 'vms', name: 'vms', component: VmsView, meta: { title: 'Virtuelle Maschinen' } },
        { path: 'vms/:name', name: 'vm-config', component: VmConfigView, meta: { title: 'VM-Einstellungen' } },
        {
          path: 'einstellungen',
          component: SettingsView,
          meta: { title: 'Einstellungen' },
          children: [
            { path: '', redirect: { name: 'settings-network' } },
            { path: 'netzwerk', name: 'settings-network', component: NetworkSettings, meta: { title: 'Einstellungen · Netzwerk' } },
            { path: 'benachrichtigungen', name: 'notifications', component: NotificationsView, meta: { title: 'Einstellungen · Benachrichtigungen' } },
            { path: 'sicherung', name: 'backup', component: BackupView, meta: { title: 'Einstellungen · Sicherung' } },
            { path: 'updates', name: 'updates', component: UpdatesView, meta: { title: 'Einstellungen · Updates' } },
          ],
        },
        { path: 'system/updates', redirect: '/einstellungen/updates' },
        { path: 'system/benachrichtigungen', redirect: '/einstellungen/benachrichtigungen' },
        { path: 'system/sicherung', redirect: '/einstellungen/sicherung' },
        { path: 'profil', name: 'profile', component: ProfileView, meta: { title: 'Profil' } },
        { path: 'benutzer', name: 'users', component: UsersGroupsView, meta: { title: 'Benutzer & Gruppen' } },
        { path: 'benutzer/gruppen', redirect: { name: 'users', query: { tab: 'gruppen' } } },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

router.beforeEach(async (to) => {
  if (to.meta.public) return true
  const auth = useAuthStore()
  if (auth.user || (await auth.fetchMe())) return (await useSetupStore().firstRunRedirect(to.path)) ?? true
  return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : {} }
})

setUnauthorizedHandler(() => {
  const auth = useAuthStore()
  auth.user = null
  if (router.currentRoute.value.name !== 'login') {
    router.replace({ name: 'login', query: { next: router.currentRoute.value.fullPath } })
  }
})
