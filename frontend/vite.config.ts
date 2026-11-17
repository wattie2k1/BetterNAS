import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// Im Entwicklungsmodus leitet Vite /api an die API im Dev-Modus weiter (betternas-api --dev).
const target = process.env.BNAS_API ?? 'http://192.168.1.54:8080'

export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  // noVNC nutzt Top-Level-await.
  build: { target: 'es2022' },
  server: { proxy: { '/api': { target, ws: true, changeOrigin: false } } },
  test: { environment: 'jsdom', globals: true, setupFiles: ['src/test/setup.ts'] },
})
