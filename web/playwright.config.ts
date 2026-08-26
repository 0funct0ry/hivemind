import { defineConfig, devices } from '@playwright/test'

const DATA_DIR = process.env.HIVEMIND_E2E_DATA_DIR ?? '/tmp/hivemind-e2e'
const PORT = process.env.HIVEMIND_E2E_PORT ?? '8098'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: 'list',
  use: {
    baseURL: `http://localhost:${PORT}`,
    trace: 'on-first-retry',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `../bin/hivemind serve --data-dir ${DATA_DIR} --addr :${PORT}`,
    url: `http://localhost:${PORT}`,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
})
