// S8: Playwright suite for the React SPA. Replaces the legacy hash-route SPA
// suite. Uses the cookie session + CSRF flow the React frontend actually uses.
const { test, expect } = require('@playwright/test');

const adminUsername = process.env.VEIL_BROWSER_USERNAME || 'browser-admin';
const adminPassword = process.env.VEIL_BROWSER_PASSWORD || 'Browser-E2E-Password-123!';

async function login(page, username, password) {
  await page.goto('/');
  // The React SPA shows a sign-in form when unauthenticated.
  await expect(page.locator('#login-username')).toBeVisible({ timeout: 15_000 });
  await page.locator('#login-username').fill(username);
  await page.locator('#login-password').fill(password);
  await page.getByRole('button', { name: /^sign in$/i }).click();
  // The panel rate-limits auth attempts (100/min per source). If a burst of
  // tests trips it, the SPA shows a transient invalid-credentials error; wait
  // for the limiter window to pass and retry rather than failing the test.
  const nav = page.getByRole('link', { name: /clients/i }).first();
  const alert = page.locator('[role="alert"]');
  try {
    await expect(nav).toBeVisible({ timeout: 20_000 });
  } catch {
    if (await alert.isVisible().catch(() => false)) {
      await page.waitForTimeout(15_000);
      await page.getByRole('button', { name: /^sign in$/i }).click();
    }
    await expect(nav).toBeVisible({ timeout: 20_000 });
  }
}

test.describe('Veil Panel — React SPA', () => {
  test('shows the sign-in form when unauthenticated', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#login-username')).toBeVisible({ timeout: 15_000 });
    await expect(page.locator('#login-password')).toBeVisible();
    await expect(page.getByRole('button', { name: /^sign in$/i })).toBeVisible();
  });

  test('rejects invalid credentials', async ({ page }) => {
    await page.goto('/');
    await page.locator('#login-username').fill('nobody');
    await page.locator('#login-password').fill('wrong-password');
    await page.getByRole('button', { name: /^sign in$/i }).click();
    await expect(page.locator('[role="alert"]')).toBeVisible({ timeout: 10_000 });
  });

  test('admin can sign in and reach the clients page', async ({ page }) => {
    await login(page, adminUsername, adminPassword);
    await page.getByRole('link', { name: /clients/i }).first().click();
    await expect(page).toHaveURL(/\/clients/);
    // The clients table header renders (name column).
    await expect(page.getByRole('columnheader', { name: /name/i }).first()).toBeVisible({ timeout: 10_000 });
  });

  test('admin can open the inbounds page', async ({ page }) => {
    await login(page, adminUsername, adminPassword);
    await page.getByRole('link', { name: /inbounds/i }).first().click();
    await expect(page).toHaveURL(/\/inbounds/);
    await expect(page.getByRole('button', { name: /new inbound/i })).toBeVisible({ timeout: 10_000 });
  });

  test('admin can create a client through the wizard', async ({ page }) => {
    await login(page, adminUsername, adminPassword);
    await page.getByRole('link', { name: /clients/i }).first().click();
    const name = `e2e-client-${Date.now()}`;

    // Open the new-client wizard (a button, not a link).
    await page.getByRole('button', { name: /new client/i }).click();
    await expect(page).toHaveURL(/\/clients\/new/);
    // Step 0: identity. The name field is required to advance.
    await page.locator('#nc-name').fill(name);
    await page.getByRole('button', { name: /^next$/i }).click();
    // Step 1: inbounds (may be empty on a fresh panel).
    await page.getByRole('button', { name: /^next$/i }).click();
    // Step 2: credentials -> review. The Review action submits the create and
    // lands on the issued-credentials screen ("Client created" + Done).
    await page.getByRole('button', { name: /^review$/i }).click();
    const done = page.getByRole('button', { name: /^done$/i }).first();
    await expect(done).toBeVisible({ timeout: 15_000 });
    await done.click();

    // Return to the clients list and confirm the client appears.
    await page.getByRole('link', { name: /clients/i }).first().click();
    await expect(
      page.getByRole('row', { name: new RegExp(name) }),
    ).toBeVisible({ timeout: 10_000 });
  });

  test('sign out returns to the login form', async ({ page }) => {
    await login(page, adminUsername, adminPassword);
    await page.getByRole('button', { name: /^logout$/i }).first().click();
    await expect(page.locator('#login-username')).toBeVisible({ timeout: 10_000 });
  });
});
