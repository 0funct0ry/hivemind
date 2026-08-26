import type { Page } from '@playwright/test'

export const E2E_USER = 'user1'
export const E2E_PASSWORD = 'e2e-test-pass-123'

export async function login(page: Page): Promise<void> {
  await page.goto('/login')
  await page.getByRole('textbox').first().fill(E2E_USER)
  await page.locator('input[type="password"]').fill(E2E_PASSWORD)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL((url) => url.pathname === '/')
}

export function isMac(): boolean {
  return process.platform === 'darwin'
}

/** Platform-appropriate modifier for ⌘/Ctrl shortcuts. */
export const MOD = isMac() ? 'Meta' : 'Control'
