import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  // Assets and the app root are served under /aks-watcher/ in production.
  // The dev server is unaffected — the Vite proxy still rewrites /api locally.
  base: '/aks-watcher/',
  plugins: [react()],
  server: {
    port: 5173,
    // Proxy /api calls to the Go backend during local development so you
    // never have to hard-code the backend URL or deal with CORS in dev.
    proxy: {
      '/aks-watcher/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
