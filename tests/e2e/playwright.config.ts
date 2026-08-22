import path from 'node:path';
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './specs',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'list',
  use: {
    baseURL: process.env.HIVE_BASE_URL || 'http://localhost:8080',
    trace: 'on-first-retry',
  },
  // Serves the SPA for the UI-driven specs (domains, tunnels). API-level specs
  // keep using baseURL (the backend). Set HIVE_UI_URL to override the UI origin.
  webServer: process.env.HIVE_NO_UI_SERVER
    ? undefined
    : {
        command: 'npm run dev -- --port 5180 --strictPort',
        cwd: path.join(__dirname, '../../ui'),
        port: 5180,
        reuseExistingServer: !process.env.CI,
        timeout: 60_000,
      },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
});
