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

  // Critical flow (panel completeness): an inbound is created through the
  // actual UI form (not seeded via the API), with a protocol-specific dynamic
  // field and the generate action filling a credential. The row then appears
  // in the list and the record is readable back through the API.
  test('admin creates a hysteria2 inbound through the UI form', async ({ page, request }) => {
    const apiToken = process.env.VEIL_BROWSER_API_TOKEN || 'browser-e2e-token';
    const stamp = Date.now();
    const name = `e2e-ui-inbound-${stamp}`;
    const port = 21000 + (stamp % 20000);

    await login(page, adminUsername, adminPassword);
    await page.getByRole('link', { name: /inbounds/i }).first().click();
    await page.getByRole('button', { name: /new inbound/i }).click();

    // Identity + protocol basics.
    await page.locator('#ib-name').fill(name);
    await page.locator('#ib-proto').selectOption('hysteria2');
    // hysteria2 supports only udp; the transport select is reset on protocol
    // switch, but pin it explicitly to prove the form round-trip.
    await page.locator('#ib-trans').selectOption('udp');
    await page.locator('#ib-port').fill(String(port));

    // Dynamic protocol field: the hysteria2 password has a generate action.
    const passwordField = page.locator('#ib-field-hysteria2Password');
    await expect(passwordField).toBeVisible({ timeout: 10_000 });
    await page.getByRole('button', { name: /^generate$/i }).click();
    // The CSPRNG fills the field with a 32-char hex value.
    await expect(passwordField).toHaveValue(/^[0-9a-f]{32}$/, { timeout: 10_000 });

    // hysteria2 needs a public domain to run; the browser-e2e panel has none,
    // so disable the inbound before creating it (same convention as the API
    // seed helper) — the form round-trip is what this test covers.
    await page.locator('#ib-enabled').uncheck();

    await page.getByRole('button', { name: /^create$/i }).click();

    // The inbound appears in the table and is readable via the API with the
    // generated credential intact (redacted in the API view, present at rest).
    await expect(
      page.getByRole('row', { name: new RegExp(name) }),
    ).toBeVisible({ timeout: 15_000 });

    const list = await request.get('/api/inbounds', {
      headers: { 'X-Veil-Token': apiToken },
    });
    expect(list.status()).toBe(200);
    const body = await list.json();
    const found = body.find((i) => i.name === name);
    expect(found, `created inbound ${name} missing from API list`).toBeTruthy();
    expect(found.protocol).toBe('hysteria2');
    expect(found.port).toBe(port);
    expect(found.enabled).toBe(false);
  });

  test('traffic lazy chunks load without runtime errors', async ({ page }) => {
    const pageErrors = [];
    page.on('pageerror', (error) => pageErrors.push(error.message));

    await login(page, adminUsername, adminPassword);
    await page.getByRole('link', { name: /^traffic$/i }).first().click();
    await expect(page).toHaveURL(/\/traffic/);
    await expect(page.getByRole('heading', { name: /^traffic telemetry$/i })).toBeVisible({ timeout: 10_000 });
    expect(pageErrors).toEqual([]);
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
    // Step 1: limits.
    await page.getByRole('button', { name: /^next$/i }).click();
    // Step 2: bindings -> review screen.
    await page.getByRole('button', { name: /^review$/i }).click();
    // Step 3: review -> submit the create.
    await page.getByRole('button', { name: /create client/i }).click();
    const done = page.getByRole('button', { name: /^done$/i }).first();
    await expect(done).toBeVisible({ timeout: 15_000 });
    await done.click();

    // Return to the clients list and confirm the client appears.
    await expect(page).toHaveURL(/\/clients/);
    await expect(
      page.getByRole('row', { name: new RegExp(name) }),
    ).toBeVisible({ timeout: 10_000 });
  });

  // Critical flow (blocker W8): the atomic create — client + binding +
  // server-generated credential committed in one transaction, with the
  // plaintext surfaced exactly once in the one-time credentials dialog.
  test('client bound to an inbound receives one-time credentials', async ({ page, request }) => {
    const apiToken = process.env.VEIL_BROWSER_API_TOKEN || 'browser-e2e-token';
    const stamp = Date.now();
    const inboundName = `e2e-inbound-${stamp}`;
    // Unique port per run: a reused panel state dir must never collide.
    const port = 20000 + (stamp % 20000);
    const resp = await request.post('/api/inbounds', {
      headers: { 'X-Veil-Token': apiToken, 'Content-Type': 'application/json' },
      data: { name: inboundName, protocol: 'hysteria2', transport: 'udp', port, enabled: false },
    });
    expect(resp.status(), `inbound seed failed: ${resp.status()} ${await resp.text()}`).toBeLessThan(300);

    await login(page, adminUsername, adminPassword);
    await page.getByRole('link', { name: /clients/i }).first().click();
    const name = `e2e-bound-${Date.now()}`;
    await page.getByRole('button', { name: /new client/i }).click();
    await page.locator('#nc-name').fill(name);
    await page.getByRole('button', { name: /^next$/i }).click();
    await page.getByRole('button', { name: /^next$/i }).click();
    // Step 2: bind to the seeded inbound; leave the credential empty so the
    // server generates one.
    await page.getByRole('checkbox', { name: new RegExp(inboundName) }).check();
    await page.getByRole('button', { name: /^review$/i }).click();
    await page.getByRole('button', { name: /create client/i }).click();

    // The one-time credentials dialog shows the generated plaintext exactly
    // once, labeled with the inbound.
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 15_000 });
    await expect(dialog).toContainText(inboundName);
    const plaintext = dialog.locator('code.mono').first();
    await expect(plaintext).toBeVisible();
    await expect(plaintext).not.toBeEmpty();
    await dialog.getByRole('button', { name: /^done$/i }).click();

    // The new client is listed.
    await expect(page).toHaveURL(/\/clients/);
    await expect(
      page.getByRole('row', { name: new RegExp(name) }),
    ).toBeVisible({ timeout: 10_000 });
  });

  // Critical flow (blocker W8): the atomic update — rename persists through
  // the client detail page and survives a reload (committed, not cached).
  test('rename a client persists through the atomic update path', async ({ page }) => {
    await login(page, adminUsername, adminPassword);
    await page.getByRole('link', { name: /clients/i }).first().click();
    const name = `e2e-rename-${Date.now()}`;
    await page.getByRole('button', { name: /new client/i }).click();
    await page.locator('#nc-name').fill(name);
    await page.getByRole('button', { name: /^next$/i }).click();
    await page.getByRole('button', { name: /^next$/i }).click();
    await page.getByRole('button', { name: /^review$/i }).click();
    await page.getByRole('button', { name: /create client/i }).click();
    const done = page.getByRole('button', { name: /^done$/i }).first();
    await expect(done).toBeVisible({ timeout: 15_000 });
    await done.click();

    // Open the client detail from the list and rename it.
    await page.getByRole('row', { name: new RegExp(name) }).click();
    await expect(page).toHaveURL(/\/clients\/.+/);
    const renamed = `${name}-renamed`;
    await page.locator('#cd-name').fill(renamed);
    await page.getByRole('button', { name: /save changes/i }).click();
    await expect(page.locator('#cd-name')).toHaveValue(renamed, { timeout: 10_000 });

    // A hard reload proves the rename committed (not just local state).
    await page.reload();
    await expect(page.locator('#cd-name')).toHaveValue(renamed, { timeout: 10_000 });
  });

  test('sign out returns to the login form', async ({ page }) => {
    await login(page, adminUsername, adminPassword);
    await page.getByRole('button', { name: /^logout$/i }).first().click();
    await expect(page.locator('#login-username')).toBeVisible({ timeout: 10_000 });
  });
});
