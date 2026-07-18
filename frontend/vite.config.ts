import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Admin SPA is served by the Go proxy under /admin/ and embedded from proxy/webdist.
export default defineConfig({
  base: '/admin/',
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../proxy/webdist',
    emptyOutDir: true,
  },
  server: {
    // dev proxy: forward API calls to the running Go proxy
    proxy: {
      '/admin/api': 'http://127.0.0.1:5000',
    },
  },
})
