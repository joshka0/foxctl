import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

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
      '/api': 'http://localhost:8090',
      '/terminal': 'http://localhost:8090',
      '/static': 'http://localhost:8090',
      '/ws': {
        target: 'http://localhost:8090',
        ws: true,
      },
    },
  },
})
