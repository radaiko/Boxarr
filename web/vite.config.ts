import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Static SPA built to dist/ and embedded into the Go binary (internal/web).
export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
})
