import { createRouter, createWebHistory } from 'vue-router'
import { setUnauthorizedHandler } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { SETUP_PATH, useSetupStore } from '@/stores/setup'
import AppLayout from '@/components/AppLayout.vue'
import LoginView from '@/views/LoginView.vue'
import DashboardView from '@/views/DashboardView.vue'
import DisksView from '@/views/storage/DisksView.vue'
import UsersView from '@/views/users/UsersView.vue'
import UpdatesView from '@/views/system/UpdatesView.vue'
import SetupView from '@/views/SetupView.vue'
import SharesView from '@/views/shares/SharesView.vue'
import GroupsView from '@/views/users/GroupsView.vue'
import PoolsView from '@/views/storage/PoolsView.vue'
import StacksView from '@/views/docker/StacksView.vue'
import ContainersView from '@/views/docker/ContainersView.vue'

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
        { path: 'system/updates', name: 'updates', component: UpdatesView, meta: { title: 'Updates' } },
        { path: 'benutzer', name: 'users', component: UsersView, meta: { title: 'Benutzer' } },
        { path: 'benutzer/gruppen', name: 'groups', component: GroupsView, meta: { title: 'Gruppen' } },
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
