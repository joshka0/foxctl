import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

const apiTarget = process.env.FOXCTL_GUI_API_TARGET || 'http://localhost:8090'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5174,
    proxy: {
      '/api': apiTarget,
      '/terminal': apiTarget,
      '/static': apiTarget,
      '/ws': {
        target: apiTarget,
        ws: true,
      },
    },
  },
})
