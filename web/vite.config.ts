import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

const backendHost = process.env.BACKEND_HOST ?? 'localhost'
const backendPort = process.env.BACKEND_PORT ?? '8080'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    host: '0.0.0.0',
    watch: {
      usePolling: true,
      interval: 200,
    },
    proxy: {
      '/ws': {
        target: `ws://${backendHost}:${backendPort}`,
        ws: true,
        changeOrigin: true,
      },
    },
  },
})
