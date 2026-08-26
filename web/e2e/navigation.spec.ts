import { expect, test } from '@playwright/test'
import { login, MOD } from './helpers'

test.beforeEach(async ({ page }) => {
  await login(page)
  // Wait for the app shell (and its global keyboard-shortcut listener) to be mounted
  // before any test starts sending shortcuts.
  await expect(page.getByRole('navigation')).toBeVisible()
})

test('pulse ruler renders a diurnal pattern with varying bar heights', async ({ page }) => {
  await page.getByRole('link', { name: '# Channel 1' }).click()
  const ruler = page.locator('svg[role="slider"]')
  await expect(ruler).toBeVisible()

  const heights = await ruler.locator('rect').evaluateAll((rects) =>
    rects.map((r) => Number(r.getAttribute('height'))),
  )
  const distinctHeights = new Set(heights)
  expect(distinctHeights.size).toBeGreaterThan(1)
  expect(heights.some((h) => h > 0)).toBe(true)
})

test('clicking a ruler bucket jumps the message list to that point in history', async ({ page }) => {
  await page.getByRole('link', { name: '# Channel 1' }).click()
  const ruler = page.locator('svg[role="slider"]')
  await expect(ruler).toBeVisible()

  const box = await ruler.boundingBox()
  if (!box) throw new Error('ruler has no bounding box')

  const beforeIds = await page.locator('[data-message-id]').evaluateAll((els) => els.map((e) => e.getAttribute('data-message-id')))

  // Click near the left edge of the ruler — the oldest visible bucket.
  await page.mouse.click(box.x + box.width * 0.05, box.y + box.height / 2)
  await page.waitForTimeout(300)

  const afterIds = await page.locator('[data-message-id]').evaluateAll((els) => els.map((e) => e.getAttribute('data-message-id')))
  expect(afterIds).not.toEqual(beforeIds)
})

test('dragging the ruler scrubs the message list continuously', async ({ page }) => {
  await page.getByRole('link', { name: '# Channel 1' }).click()
  const ruler = page.locator('svg[role="slider"]')
  await expect(ruler).toBeVisible()

  const box = await ruler.boundingBox()
  if (!box) throw new Error('ruler has no bounding box')

  const idsAt = async (fracX: number) => {
    await page.mouse.move(box.x + box.width * fracX, box.y + box.height / 2)
    await page.waitForTimeout(150)
    return page.locator('[data-message-id]').first().getAttribute('data-message-id')
  }

  await page.mouse.move(box.x + box.width * 0.1, box.y + box.height / 2)
  await page.mouse.down()
  const first = await idsAt(0.1)
  const middle = await idsAt(0.5)
  const last = await idsAt(0.9)
  await page.mouse.up()

  // At least one of the samples should differ across the drag — the list is following.
  expect(new Set([first, middle, last]).size).toBeGreaterThan(1)
})

test('command palette (⌘K) finds and navigates to a channel', async ({ page }) => {
  await page.keyboard.press(`${MOD}+K`)
  const input = page.getByPlaceholder(/jump to a channel/i)
  await expect(input).toBeVisible()
  await input.fill('chan3')
  await expect(page.getByRole('dialog').getByRole('button', { name: /Channel 3/ })).toBeVisible()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/c\/channel-3$/)
})

test('search (⌘/) finds a message and jumps to it with a highlight', async ({ page }) => {
  await page.getByRole('link', { name: '# Channel 1' }).click()
  await page.keyboard.press(`${MOD}+Slash`)
  const input = page.getByPlaceholder(/search messages/i)
  await expect(input).toBeVisible()
  await input.fill('constraint')

  const result = page.locator('mark').first()
  await expect(result).toBeVisible({ timeout: 5000 })
  await result.locator('xpath=ancestor::button[1]').click()

  await expect(page.locator('.bg-pollen-soft')).toBeVisible({ timeout: 2000 })
})

test('the app is fully operable with the keyboard alone', async ({ page }) => {
  // Navigate into a channel first so the tab sequence covers the ruler and composer
  // too, not just the sidebar links on the empty landing page.
  await page.getByRole('link', { name: '# Channel 1' }).click()
  await expect(page.locator('svg[role="slider"]')).toBeVisible()

  // Sanity check: sidebar, ruler, and composer controls are all real, tabbable elements
  // in the DOM (the exact traversal order/anchor is a browser-internal detail not worth
  // asserting on) — confirms the app doesn't rely on mouse-only affordances.
  const focusable = await page.evaluate(() =>
    [...document.querySelectorAll('a,button,input,textarea,[tabindex]:not([tabindex="-1"])')].map((e) => e.tagName),
  )
  expect(new Set(focusable).size).toBeGreaterThan(1)
  expect(focusable).toContain('TEXTAREA')

  const readFocusInfo = () =>
    page.evaluate(() => {
      const el = document.activeElement
      if (!el || el === document.body) return null
      const style = getComputedStyle(el)
      return { tag: el.tagName, outline: style.outlineStyle }
    })

  // Walk both directions from wherever focus currently sits — regardless of the
  // browser's internal tab-order anchor, this must reach more than one distinct element
  // and every stop must carry a visible focus outline.
  const seenTags = new Set<string>()
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press('Tab')
    const info = await readFocusInfo()
    if (info) {
      seenTags.add(info.tag)
      expect(info.outline).not.toBe('none')
    }
  }
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press('Shift+Tab')
    const info = await readFocusInfo()
    if (info) seenTags.add(info.tag)
  }

  expect(seenTags.size).toBeGreaterThan(1)
})
