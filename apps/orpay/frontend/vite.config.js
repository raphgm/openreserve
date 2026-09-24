import { defineConfig } from 'vite'

// In development, proxy the node API and the ORPay backend so the app is
// served from one origin.
export default defineConfig({
  server: {
    port: 5173,
    proxy: {
      '/v1': process.env.ORP_NODE ?? 'http://localhost:8080',
      '/api': process.env.ORPAY_BACKEND ?? 'http://localhost:4000',
    },
  },
})
