const { test, expect } = require('@playwright/test');

const adminUsername = process.env.VEIL_BROWSER_USERNAME || 'browser-admin';
const adminPassword = process.env.VEIL_BROWSER_PASSWORD || 'Browser-E2E-Password-123!';
const viewerUsername = process.env.VEIL_BROWSER_VIEWER_USERNAME || 'browser-viewer';
const viewerPassword = process.env.VEIL_BROWSER_VIEWER_PASSWORD || 'Browser-E2E-Viewer-Password-123!';

async function login(page, username, password, expectedRole) {
  await page.goto('/');
  await expect(page).toHaveTitle(/Login.+Veil Panel/);
  await page.locator('#username').fill(username);
  await page.locator('#password').fill(password);
  await page.locator('#login-submit').click();
  await expect(page.locator('#add-inbound-btn')).toBeAttached();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('veil_csrf_token'))).not.toBeNull();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('veil_user_role'))).toBe(expectedRole);
}

async function ensureViewerExists(page) {
  const result = await page.evaluate(async ({ username, password }) => {
    const csrf = localStorage.getItem('veil_csrf_token') || '';
    const headers = {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrf,
    };
    const create = await fetch('/api/users', {
      method: 'POST',
      credentials: 'same-origin',
      headers,
      body: JSON.stringify({ username, password, role: 'viewer', locale: 'en' }),
    });
    if (create.ok) return { method: 'POST', status: create.status };
    const update = await fetch(`/api/users/${encodeURIComponent(username)}`, {
      method: 'PUT',
      credentials: 'same-origin',
      headers,
      body: JSON.stringify({ password, role: 'viewer', locale: 'en' }),
    });
    return { method: 'PUT', status: update.status };
  }, { username: viewerUsername, password: viewerPassword });
  expect(result.status).toBeGreaterThanOrEqual(200);
  expect(result.status).toBeLessThan(300);
}

function watchBrowserFailures(page) {
  const pageErrors = [];
  const serverErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('response', (response) => {
    if (response.status() < 500) return;
    const url = new URL(response.url());
    const expectedRuntimeUnavailable =
      response.status() === 503 &&
      response.request().method() === 'GET' &&
      url.pathname.endsWith('/api/status');
    if (!expectedRuntimeUnavailable) {
      serverErrors.push(`${response.status()} ${response.request().method()} ${response.url()}`);
    }
  });
  return () => {
    expect(pageErrors).toEqual([]);
    expect(serverErrors).toEqual([]);
  };
}

async function buildApplyPlan(page) {
  await page.locator('a[href="#diagnostics"]').click();
  await expect(page.locator('#build-apply-plan')).toBeVisible();
  const responsePromise = page.waitForResponse((response) =>
    response.url().endsWith('/api/apply/plan') && response.request().method() === 'POST'
  );
  await page.locator('#build-apply-plan').click();
  const response = await responsePromise;
  expect(response.status()).toBe(200);
  const body = await response.json();
  expect(body.plan ? body.plan.valid : body.valid).toBe(true);
  await expect(page.locator('#apply-plan-output')).toContainText(/valid/i);
  return body;
}

async function openInboundAction(page, row, name, action) {
  await row.locator('.dropdown-btn').click();
  const actionButton = page.locator(
    `[data-inbound-action="${action}"][data-inbound-name="${name}"]`
  ).last();
  await expect(actionButton).toBeVisible();
  await actionButton.click();
}

async function expectRestrictedNavigation(link) {
  await expect(link).toHaveAttribute('hidden', '');
  await expect(link).toHaveAttribute('aria-hidden', 'true');
  await expect(link).toHaveAttribute('tabindex', '-1');
}

test('invalid login remains recoverable', async ({ page }) => {
  const assertNoBrowserFailures = watchBrowserFailures(page);
  await page.goto('/');
  await page.locator('#username').fill(adminUsername);
  await page.locator('#password').fill('incorrect-password');
  await page.locator('#login-submit').click();
  await expect(page.locator('#error')).toBeVisible();
  await expect(page.locator('#error')).toContainText(/invalid username or password/i);
  await expect(page.locator('#login-submit')).toBeEnabled();
  assertNoBrowserFailures();
});

test('admin inbound lifecycle invalidates stale apply plans', async ({ page }, testInfo) => {
  const assertNoBrowserFailures = watchBrowserFailures(page);
  const runIndex = (testInfo.repeatEachIndex * 2) + testInfo.retry;
  const inboundName = `browser-mieru-${testInfo.repeatEachIndex}-${testInfo.retry}`;
  const initialPort = 18443 + runIndex;
  const updatedPort = 19443 + runIndex;

  await login(page, adminUsername, adminPassword, 'admin');

  await expect(page.locator('#apply-staged-files')).toBeDisabled();
  await expect(page.locator('#apply-live-configs')).toBeDisabled();
  await expect(page.locator('#reload-services')).toBeDisabled();

  await page.locator('a[href="#inbounds"]').click();
  await page.locator('#add-inbound-btn').click();
  await expect(page.locator('#inbound-modal-overlay')).toHaveAttribute('aria-hidden', 'false');

  await page.locator('#inbound-name').fill(inboundName);
  await page.locator('#inbound-protocol').selectOption('mieru');
  await page.locator('#inbound-transport').selectOption('tcp');
  await page.locator('#inbound-port').fill(String(initialPort));
  await page.locator('#inbound-password').fill('browser-mieru-password');
  const enabled = page.locator('#inbound-enabled');
  await enabled.locator('..').click();
  await expect(enabled).not.toBeChecked();
  await expect(page.locator('#save-inbound')).toBeEnabled();

  const createResponsePromise = page.waitForResponse((response) =>
    response.url().endsWith('/api/inbounds') && response.request().method() === 'POST'
  );
  await page.locator('#save-inbound').click();
  expect((await createResponsePromise).status()).toBe(201);

  let inboundRow = page.locator('#inbounds-tbody tr').filter({ hasText: inboundName });
  await expect(inboundRow).toContainText(String(initialPort));
  await expect(inboundRow.locator('input[type="checkbox"]')).not.toBeChecked();

  await buildApplyPlan(page);
  await expect(page.locator('#apply-staged-files')).toBeEnabled();
  await expect(page.locator('#apply-live-configs')).toBeEnabled();
  await expect(page.locator('#reload-services')).toBeEnabled();

  await page.locator('a[href="#inbounds"]').click();
  inboundRow = page.locator('#inbounds-tbody tr').filter({ hasText: inboundName });
  await openInboundAction(page, inboundRow, inboundName, 'edit');
  await expect(page.locator('#inbound-modal-overlay')).toHaveAttribute('aria-hidden', 'false');
  await expect(page.locator('#inbound-name')).toHaveValue(inboundName);
  await page.locator('#inbound-port').fill(String(updatedPort));
  await expect(page.locator('#save-inbound')).toBeEnabled();

  const updateResponsePromise = page.waitForResponse((response) =>
    response.url().endsWith(`/api/inbounds/${encodeURIComponent(inboundName)}`) &&
    response.request().method() === 'PUT'
  );
  await page.locator('#save-inbound').click();
  expect((await updateResponsePromise).status()).toBe(200);

  inboundRow = page.locator('#inbounds-tbody tr').filter({ hasText: inboundName });
  await expect(inboundRow).toContainText(String(updatedPort));
  await expect(page.locator('#apply-staged-files')).toBeDisabled();
  await expect(page.locator('#apply-live-configs')).toBeDisabled();
  await expect(page.locator('#reload-services')).toBeDisabled();

  await buildApplyPlan(page);
  await expect(page.locator('#apply-staged-files')).toBeEnabled();

  await page.locator('a[href="#inbounds"]').click();
  inboundRow = page.locator('#inbounds-tbody tr').filter({ hasText: inboundName });
  page.once('dialog', (dialog) => dialog.accept());
  const deleteResponsePromise = page.waitForResponse((response) =>
    response.url().endsWith(`/api/inbounds/${encodeURIComponent(inboundName)}`) &&
    response.request().method() === 'DELETE'
  );
  await openInboundAction(page, inboundRow, inboundName, 'delete');
  expect([200, 204]).toContain((await deleteResponsePromise).status());
  await expect(page.locator('#inbounds-tbody tr').filter({ hasText: inboundName })).toHaveCount(0);
  await expect(page.locator('#apply-staged-files')).toBeDisabled();
  await expect(page.locator('#apply-live-configs')).toBeDisabled();
  await expect(page.locator('#reload-services')).toBeDisabled();

  assertNoBrowserFailures();
});

test('viewer is read-only, may preview a plan, and can log out', async ({ page }) => {
  const assertNoBrowserFailures = watchBrowserFailures(page);
  await login(page, adminUsername, adminPassword, 'admin');
  await ensureViewerExists(page);
  await page.locator('#btn-logout').click();
  await expect(page).toHaveTitle(/Login.+Veil Panel/);

  await login(page, viewerUsername, viewerPassword, 'viewer');

  await expectRestrictedNavigation(page.locator('a[href="#users"]'));
  await expectRestrictedNavigation(page.locator('a[href="#backups"]'));
  await expect(page.locator('#users')).toHaveAttribute('hidden', '');
  await expect(page.locator('#backups')).toHaveAttribute('hidden', '');
  await expect(page.locator('#add-inbound-btn')).toBeDisabled();

  await page.goto('/#users');
  await expect(page).toHaveURL(/#dashboard$/);
  await expect(page.locator('#users')).toHaveAttribute('hidden', '');

  await buildApplyPlan(page);
  await expect(page.locator('#apply-staged-files')).toBeDisabled();
  await expect(page.locator('#apply-live-configs')).toBeDisabled();
  await expect(page.locator('#reload-services')).toBeDisabled();

  const forbiddenMutationStatus = await page.evaluate(async () => {
    const csrf = localStorage.getItem('veil_csrf_token') || '';
    const response = await fetch('/api/inbounds', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': csrf,
      },
      body: JSON.stringify({
        name: 'viewer-forbidden',
        protocol: 'mieru',
        transport: 'tcp',
        port: 28443,
        password: 'viewer-forbidden-password',
        enabled: false,
      }),
    });
    return response.status;
  });
  expect(forbiddenMutationStatus).toBe(403);

  await page.locator('#btn-logout').click();
  await expect(page).toHaveTitle(/Login.+Veil Panel/);
  await expect.poll(() => page.evaluate(() => localStorage.getItem('veil_csrf_token'))).toBeNull();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('veil_user_role'))).toBeNull();
  assertNoBrowserFailures();
});
