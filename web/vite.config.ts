import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    // Build straight into internal/web/dist so `//go:embed all:dist` in
    // internal/web/embed.go can pick it up without a copy step.
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
})
