import { randomBytes } from 'crypto';
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  timeout: 30_000,
  expect: {
    timeout: 10_000,
  },
  fullyParallel: false,
  retries: 1,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: 'http://localhost:18080',
    trace: 'on-first-retry',
  },
  webServer: {
    command: 'go run .',
    cwd: '..',
    url: 'http://localhost:18080',
    env: { CSRF_KEY: randomBytes(32).toString('hex') },
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
