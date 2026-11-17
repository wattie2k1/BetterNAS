import { enableAutoUnmount } from '@vue/test-utils'
import { afterEach } from 'vitest'

// Gemountete Komponenten nach jedem Test aushängen, damit deren Timer (z. B. PrimeVue-TabList) nicht
// nach dem Abbau der Testumgebung feuern („HTMLElement is not defined“).
enableAutoUnmount(afterEach)

// jsdom kennt kein ResizeObserver; PrimeVue-Tabs beobachten damit ihre Größe.
if (!('ResizeObserver' in globalThis)) {
  ;(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

// jsdom kennt kein matchMedia; PrimeVue-Komponenten (z. B. Select) fragen es ab.
if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList
}

// Node bringt ein eigenes (unbrauchbares) localStorage mit, das jsdoms überdeckt; ein einfacher Ersatz genügt.
if (typeof globalThis.localStorage?.clear !== 'function') {
  const data = new Map<string, string>()
  const storage = {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, String(v)),
    removeItem: (k: string) => void data.delete(k),
    clear: () => data.clear(),
    key: (i: number) => [...data.keys()][i] ?? null,
    get length() {
      return data.size
    },
  }
  Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true })
  Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
}
