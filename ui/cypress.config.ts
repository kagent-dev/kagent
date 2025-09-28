import { defineConfig } from 'cypress'

export default defineConfig({
  e2e: {
    baseUrl: 'http://localhost:8001',
    setupNodeEvents(on, config) {
    },
    supportFile: false
  },
}) 