<script setup lang="ts">
// Rahmen der Einstellungen: links die Bereiche nach Gruppen, rechts der gewählte Bereich.
const groups = [
  {
    title: 'System',
    items: [
      { label: 'Allgemein', icon: 'pi pi-sliders-h', to: '/einstellungen/allgemein' },
      { label: 'Netzwerk', icon: 'pi pi-sitemap', to: '/einstellungen/netzwerk' },
      { label: 'Webzugriff', icon: 'pi pi-lock', to: '/einstellungen/webzugriff' },
      { label: 'Energie', icon: 'pi pi-power-off', to: '/einstellungen/energie' },
    ],
  },
  {
    title: 'Dienste',
    items: [
      { label: 'Dienste', icon: 'pi pi-cog', to: '/einstellungen/dienste' },
      { label: 'Dateidienste', icon: 'pi pi-folder-open', to: '/einstellungen/dateidienste' },
      { label: 'Docker', icon: 'pi pi-clone', to: '/einstellungen/docker' },
    ],
  },
  {
    title: 'Sicherheit',
    items: [
      { label: 'Anmeldung', icon: 'pi pi-shield', to: '/einstellungen/anmeldung' },
      { label: 'Firewall', icon: 'pi pi-ban', to: '/einstellungen/firewall' },
      { label: 'SSH', icon: 'pi pi-key', to: '/einstellungen/ssh' },
    ],
  },
  {
    title: 'Wartung',
    items: [
      { label: 'Benachrichtigungen', icon: 'pi pi-bell', to: '/einstellungen/benachrichtigungen' },
      { label: 'Sicherung', icon: 'pi pi-history', to: '/einstellungen/sicherung' },
      { label: 'Updates', icon: 'pi pi-download', to: '/einstellungen/updates' },
      { label: 'Protokolle', icon: 'pi pi-list', to: '/einstellungen/protokolle' },
      { label: 'Über', icon: 'pi pi-info-circle', to: '/einstellungen/ueber' },
    ],
  },
]
</script>

<template>
  <div class="settings">
    <nav class="menu glass" aria-label="Einstellungen" data-test="settings-nav">
      <template v-for="g in groups" :key="g.title">
        <div class="group">{{ g.title }}</div>
        <RouterLink v-for="i in g.items" :key="i.to" :to="i.to" class="entry" active-class="active">
          <i :class="i.icon" />
          <span>{{ i.label }}</span>
        </RouterLink>
      </template>
    </nav>
    <section class="pane">
      <RouterView />
    </section>
  </div>
</template>

<style scoped>
.settings {
  display: grid;
  grid-template-columns: 230px minmax(0, 1fr);
  gap: 22px;
  align-items: start;
}
.menu {
  position: sticky;
  top: 22px;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.group {
  margin: 10px 10px 4px;
  font-size: 0.72rem;
  font-weight: 650;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--bn-text-soft);
}
.group:first-child {
  margin-top: 2px;
}
.entry {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  border-radius: var(--bn-radius-xs);
  color: var(--p-text-color);
  text-decoration: none;
}
.entry:hover {
  background: var(--bn-tile-hover);
}
.entry.active {
  background: var(--bn-tile-hover);
  font-weight: 650;
  box-shadow: inset 3px 0 0 var(--bn-accent-1);
}
.pane {
  min-width: 0;
}
@media (max-width: 800px) {
  .settings {
    grid-template-columns: 1fr;
  }
  .menu {
    position: static;
  }
}
</style>
