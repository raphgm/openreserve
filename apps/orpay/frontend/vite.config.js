import { resolve } from 'node:path'
import { defineConfig } from 'vite'

// In development, proxy the node API and the ORPay backend so the app is
// served from one origin.
export default defineConfig({
  build: {
    rollupOptions: {
      // The wallet and the public block explorer are separate pages.
      input: { main: resolve(__dirname, 'index.html'), explorer: resolve(__dirname, 'explorer.html') },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/v1': process.env.ORP_NODE ?? 'http://localhost:8080',
      '/api': process.env.ORPAY_BACKEND ?? 'http://localhost:4000',
      '/pay': process.env.ORPAY_GATEWAY ?? 'http://localhost:4100',
    },
  },
})
