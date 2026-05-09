/**
 * GoSSR example app — Playwright end-to-end tests
 *
 * Base URL: http://localhost:18080
 */

import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Parse the first decimal or integer number found in a string.
 * Used for extracting balance values from "Balance: $52.80".
 */
function parseFirstNumber(text: string): number {
  const m = text.match(/[\d]+(?:\.[\d]+)?/);
  return m ? parseFloat(m[0]) : NaN;
}

// ---------------------------------------------------------------------------
// 1. Static rendering — /home
// ---------------------------------------------------------------------------

test('static rendering: /home page loads with navbar and content', async ({ page }) => {
  await page.goto('/home');

  // Title rendered by root layout via Title() func
  await expect(page).toHaveTitle(/Page test title/);

  // Navbar brand
  await expect(page.locator('a.navbar-brand')).toHaveText('Example');

  // Nav links exist
  await expect(page.locator('a.nav-link[href="/home"]')).toBeVisible();
  await expect(page.locator('a.nav-link[href="/users"]')).toBeVisible();
  await expect(page.locator('a.nav-link[href="/contact"]')).toBeVisible();

  // Active class on home link
  await expect(page.locator('a.nav-link[href="/home"]')).toHaveClass(/active/);

  // Main content heading
  await expect(
    page.locator('h1', { hasText: 'GoLang Server Side Rendering' })
  ).toBeVisible();

  // visitorsOnline paragraph is present (SSR value on initial render)
  await expect(
    page.locator('p', { hasText: 'visitors online right now' })
  ).toBeVisible();

  // displayName input is present
  await expect(
    page.locator('input[type="text"][placeholder="Your name"]')
  ).toBeVisible();

  // Hi, greeting is present (empty displayName on SSR load)
  await expect(page.locator('p', { hasText: 'Hi,' })).toBeVisible();
});

// ---------------------------------------------------------------------------
// 2. Reactive balance (root layout) on /home — updates via WS
// ---------------------------------------------------------------------------

test('reactive balance: balance updates live on /home', async ({ page }) => {
  await page.goto('/home');

  // The balance span text looks like "Balance: $52.80"
  const balanceSpan = page.locator('.navbar-text', { hasText: 'Balance:' }).first();
  await expect(balanceSpan).toBeVisible();

  const initialText = await balanceSpan.innerText();
  const initialBalance = parseFirstNumber(initialText);
  expect(isNaN(initialBalance)).toBe(false);

  // Root Subscribe fires every 2-3 s. Allow 10 s for a change.
  await expect.poll(
    async () => {
      const text = await balanceSpan.innerText();
      return parseFirstNumber(text);
    },
    {
      message: 'Balance should change from initial value via server-push WS patch',
      timeout: 10_000,
      intervals: [500, 500, 500, 500, 500, 500, 1000, 1000, 1000, 1000],
    }
  ).not.toBe(initialBalance);
});

// ---------------------------------------------------------------------------
// 3. Reactive visitorsOnline — updates via WS on /home
// ---------------------------------------------------------------------------

test('reactive visitorsOnline: counter updates live on /home', async ({ page }) => {
  await page.goto('/home');

  // Strong element inside the visitors paragraph
  const counter = page
    .locator('p', { hasText: 'visitors online right now' })
    .locator('strong');
  await expect(counter).toBeVisible();

  const initialValue = parseInt(await counter.innerText(), 10);
  expect(isNaN(initialValue)).toBe(false);

  // visitorsOnline Subscribe fires every 4 s. Allow 12 s.
  await expect.poll(
    async () => parseInt(await counter.innerText(), 10),
    {
      message: 'visitorsOnline should change from initial value via server-push WS patch',
      timeout: 12_000,
      intervals: Array(12).fill(1000),
    }
  ).not.toBe(initialValue);
});

test('reactive attribute: style="color: {{ visitorsBadgeColor }}" updates live', async ({ page }) => {
  await page.goto('/home');

  const counter = page
    .locator('p', { hasText: 'visitors online right now' })
    .locator('strong');
  await expect(counter).toBeVisible();

  const initialColor = await counter.evaluate((el) => (el as HTMLElement).style.color);
  expect(initialColor).not.toBe(''); // Server rendered an inline color.

  // The badge color is recomputed on every visitorsOnline tick (4 s).
  // Across two random buckets the colour will eventually change. Allow up to 30 s.
  await expect.poll(
    async () => counter.evaluate((el) => (el as HTMLElement).style.color),
    {
      message: 'style="color: ..." attribute should change via server-push WS patch',
      timeout: 30_000,
      intervals: Array(30).fill(1000),
    }
  ).not.toBe(initialColor);
});

// ---------------------------------------------------------------------------
// 4. ssr:bind two-way input — displayName wiring works end-to-end
//    The generator now emits ssr:bind="displayName" in the rendered HTML so
//    the TS runtime can wire the input and send writes over WebSocket.
// ---------------------------------------------------------------------------

test('ssr:bind displayName: typing updates the greeting Hi, NAME!', async ({ page }) => {
  await page.goto('/home');

  const input = page.locator('input[type="text"][placeholder="Your name"]');
  await expect(input).toBeVisible();

  // Verify the rendered input has the ssr:bind attribute (generator emits it).
  const ssrBindAttr = await input.getAttribute('ssr:bind');
  expect(ssrBindAttr).toBe('displayName');

  // Type a name. The TS runtime fires a WS write on blur/change.
  await input.fill('Alice');
  await input.press('Tab');

  // After the WS round-trip the server pushes a patch and the greeting updates.
  const greeting = page.locator('p', { hasText: 'Hi,' });
  await expect.poll(
    async () => (await greeting.innerText()).trim(),
    {
      message: 'Greeting should update to "Hi, Alice!" after WS write',
      timeout: 8_000,
      intervals: [300, 300, 500, 500, 1000, 1000, 1000],
    }
  ).toContain('Hi, Alice!');
});

// ---------------------------------------------------------------------------
// 5. ssr:bind validation rejection — >50 chars shows error, greeting unchanged
//    Now that BUG-1 is fixed, the full validation round-trip can be tested.
// ---------------------------------------------------------------------------

test('ssr:bind validation: >50 char name shows error and greeting is unchanged', async ({ page }) => {
  await page.goto('/home');

  const input = page.locator('input[type="text"][placeholder="Your name"]');
  await expect(input).toBeVisible();

  // First set a valid short name so the greeting is non-empty.
  await input.fill('Bob');
  await input.press('Tab');
  const greeting = page.locator('p', { hasText: 'Hi,' });
  await expect.poll(
    async () => (await greeting.innerText()).trim(),
    { timeout: 8_000, intervals: [300, 300, 500, 500, 1000, 1000, 1000] }
  ).toContain('Hi, Bob!');

  // Now submit a name that exceeds 50 characters — server should reject it.
  const longName = 'A'.repeat(51);
  await input.fill(longName);
  await input.press('Tab');

  const errorEl = page.locator('#display-name-error');
  // The error div should become non-empty with a validation message.
  await expect.poll(
    async () => (await errorEl.innerText()).trim(),
    {
      message: 'Error div should contain a validation error message after >50 char input',
      timeout: 8_000,
      intervals: [300, 300, 500, 500, 1000, 1000, 1000],
    }
  ).not.toBe('');

  // The greeting must remain "Hi, Bob!" — the rejected write must not be applied.
  const greetingText = (await greeting.innerText()).trim();
  expect(greetingText).toContain('Hi, Bob!');
  expect(greetingText).not.toContain('A'.repeat(10));
});

// ---------------------------------------------------------------------------
// 6a. Multiplexed nested page — /users/johndoe123/info — presence checks
//     These structural assertions pass regardless of BUG-2.
// ---------------------------------------------------------------------------

test('multiplexed ws: /users/johndoe123/info — all three reactive elements present in DOM', async ({ page }) => {
  await page.goto('/users/johndoe123/info');

  // (a) Root balance in navbar
  const balanceSpan = page.locator('.navbar-text', { hasText: 'Balance:' }).first();
  await expect(balanceSpan).toBeVisible();

  // (b) userCount in sidebar "N registered"
  const userCountPara = page.locator('p.text-muted.small', { hasText: 'registered' });
  await expect(userCountPara).toBeVisible();

  // (c) lastSeen on the info panel
  const lastSeenPara = page.locator('p.text-muted.small', { hasText: 'Last seen:' });
  await expect(lastSeenPara).toBeVisible();

  // (d) data-ssr-bind attributes are present for all three reactive vars
  await expect(page.locator('[data-ssr-bind="8a5edab2.balance"]').first()).toBeAttached();
  await expect(page.locator('[data-ssr-bind="954dac29.userCount"]').first()).toBeAttached();
  await expect(page.locator('[data-ssr-bind="2b2f214e.lastSeen"]').first()).toBeAttached();
});

// ---------------------------------------------------------------------------
// 6b. WS connection on /users/johndoe123/info — balance updates live (BUG-2 fixed)
//     All reactive routes now import __ssr_gen__ so the WS client connects.
//     Balance in the navbar should update over time via the multiplexed WS.
// ---------------------------------------------------------------------------

test('multiplexed ws: balance updates live on /users/johndoe123/info', async ({ page }) => {
  const wsConnections: string[] = [];
  page.on('websocket', ws => wsConnections.push(ws.url()));

  await page.goto('/users/johndoe123/info');

  // At least one WebSocket connection must be opened by the reactive client.
  await expect.poll(
    () => wsConnections.length,
    {
      message: 'At least one WebSocket should be opened by the reactive client',
      timeout: 8_000,
      intervals: [500, 500, 500, 500, 1000, 1000, 1000],
    }
  ).toBeGreaterThan(0);

  // Balance in the navbar should update via the multiplexed WS (root route Subscribe).
  const balanceSpan = page.locator('.navbar-text', { hasText: 'Balance:' }).first();
  await expect(balanceSpan).toBeVisible();
  const initialText = await balanceSpan.innerText();
  const initialBalance = parseFirstNumber(initialText);
  expect(isNaN(initialBalance)).toBe(false);

  // Root Subscribe fires every 2-3 s. Allow 12 s for a change.
  await expect.poll(
    async () => parseFirstNumber(await balanceSpan.innerText()),
    {
      message: 'Balance should change from initial value via multiplexed WS patch',
      timeout: 12_000,
      intervals: [500, 500, 500, 500, 1000, 1000, 1000, 1000, 1000],
    }
  ).not.toBe(initialBalance);
});

// ---------------------------------------------------------------------------
// 7. Contact form — empty submit shows validation errors
//    HTML5 native 'required' attribute is used on fields. Playwright/Chromium
//    enforces it client-side, so the form does not POST when fields are empty.
//    We disable native validation to exercise server-side validation.
// ---------------------------------------------------------------------------

test('contact form: empty submit (native validation disabled) shows server validation errors', async ({ page }) => {
  await page.goto('/contact');
  await expect(page.locator('h2', { hasText: 'Contact Us' })).toBeVisible();

  // Disable browser-side HTML5 form validation so the POST reaches the server
  await page.locator('form').evaluate((form: HTMLFormElement) => {
    form.setAttribute('novalidate', '');
  });

  await page.locator('button[type="submit"]').click();

  // After server-side validation, the page re-renders with error messages
  const nameError = page.locator('.invalid-feedback', { hasText: 'required' }).first();
  await expect(nameError).toBeVisible({ timeout: 5_000 });

  // Success banner must NOT appear
  await expect(page.locator('.alert-success')).not.toBeVisible();
});

// ---------------------------------------------------------------------------
// 8. Contact form — valid submit shows success banner
// ---------------------------------------------------------------------------

test('contact form: valid submit shows success banner', async ({ page }) => {
  await page.goto('/contact');

  await page.locator('#contact-name').fill('Test User');
  await page.locator('#contact-email').fill('test@example.com');
  // Select the first non-default option for topic
  await page.locator('#contact-topic').selectOption({ index: 1 });
  await page.locator('#contact-message').fill('Hello, this is a test message from Playwright.');

  await page.locator('button[type="submit"]').click();

  // Server redirects to /contact?sent=1 with the success alert
  await expect(
    page.locator('.alert-success', { hasText: 'Message sent successfully' })
  ).toBeVisible({ timeout: 5_000 });

  await expect(page.locator('.invalid-feedback')).not.toBeVisible();
});

// ---------------------------------------------------------------------------
// 9. Template feature showcase — SSR static values on /home
//    Verifies arithmetic, conditional and loop rendering are all correct
//    in the initial server-side render (no reactive involvement).
// ---------------------------------------------------------------------------

test('template showcase: arithmetic, conditionals, loops render correctly', async ({ page }) => {
  await page.goto('/home');

  // Arithmetic: price=9.99, quantity=3, total=29.97
  await expect(page.locator('li', { hasText: 'Price:' }).locator('code')).toHaveText('9.99');
  await expect(page.locator('li', { hasText: 'Quantity:' }).locator('code')).toHaveText('3');
  await expect(page.locator('li', { hasText: 'Total' }).locator('strong')).toHaveText('29.97');

  // Conditional: status="active" → "Active" badge visible
  await expect(page.locator('span.badge.bg-success', { hasText: 'Active' })).toBeVisible();
  await expect(page.locator('span.badge.bg-warning')).not.toBeVisible();
  await expect(page.locator('span.badge.bg-danger')).not.toBeVisible();

  // Loops: fruits list (index-less loop card)
  const fruitsCard = page.locator('.card-header', { hasText: 'index-less' }).locator('../..');
  await expect(fruitsCard.locator('li').nth(0)).toHaveText('Apple');
  await expect(fruitsCard.locator('li').nth(1)).toHaveText('Banana');
  await expect(fruitsCard.locator('li').nth(2)).toHaveText('Cherry');

  // Indexed loop: langs list
  const langsCard = page.locator('.card-header', { hasText: 'indexed' }).locator('../..');
  await expect(langsCard.locator('li').nth(0)).toHaveText('#0: Go');
  await expect(langsCard.locator('li').nth(1)).toHaveText('#1: TypeScript');
  await expect(langsCard.locator('li').nth(2)).toHaveText('#2: Rust');
});
