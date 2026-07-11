const { test, expect } = require('@playwright/test');

const username = process.env.VEIL_BROWSER_USERNAME || 'browser-admin';
const password = process.env.VEIL_BROWSER_PASSWORD || 'Browser-E2E-Password-123!';

async function login(page) {
  await page.goto('/');
  await expect(page).toHaveTitle(/Login.+Veil Panel/);
  await page.locator('#username').fill(username);
  await page.locator('#password').fill(password);
  await page.locator('#login-submit').click();
  await expect(page.locator('#add-inbound-btn')).toBeAttached();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('veil_csrf_token'))).not.toBeNull();
}

test('invalid login remains recoverable', async ({ page }) => {
  await page.goto('/');
  await page.locator('#username').fill(username);
  await page.locator('#password').fill('incorrect-password');
  await page.locator('#login-submit').click();
  await expect(page.locator('#error')).toBeVisible();
  await expect(page.locator('#error')).toContainText(/invalid username or password/i);
  await expect(page.locator('#login-submit')).toBeEnabled();
});

test('admin can stage a disabled inbound and build a valid apply plan', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));

  await login(page);

  await expect(page.locator('#apply-staged-files')).toBeDisabled();
  await expect(page.locator('#apply-live-configs')).toBeDisabled();
  await expect(page.locator('#reload-services')).toBeDisabled();

  await page.locator('a[href="#inbounds"]').click();
  await expect(page.locator('#add-inbound-btn')).toBeVisible();
  await page.locator('#add-inbound-btn').click();
  await expect(page.locator('#inbound-modal-overlay')).toHaveAttribute('aria-hidden', 'false');

  await page.locator('#inbound-name').fill('browser-mieru');
  await page.locator('#inbound-protocol').selectOption('mieru');
  await page.locator('#inbound-transport').selectOption('tcp');
  await page.locator('#inbound-port').fill('18443');
  await page.locator('#inbound-password').fill('browser-mieru-password');
  await page.locator('#inbound-enabled + .slider').click();
  await expect(page.locator('#inbound-enabled')).not.toBeChecked();

  const createResponsePromise = page.waitForResponse((response) =>
    response.url().endsWith('/api/inbounds') && response.request().method() === 'POST'
  );
  await page.locator('#save-inbound').click();
  const createResponse = await createResponsePromise;
  expect(createResponse.status()).toBe(201);

  await expect(page.locator('#inbounds-tbody')).toContainText('browser-mieru');
  await expect(page.locator('#inbounds-tbody')).toContainText('18443');
  await expect(page.locator('#inbounds-tbody')).toContainText(/disabled/i);

  const planResponsePromise = page.waitForResponse((response) =>
    response.url().endsWith('/api/apply/plan') && response.request().method() === 'POST'
  );
  await page.locator('#build-apply-plan').click();
  const planResponse = await planResponsePromise;
  expect(planResponse.status()).toBe(200);
  const plan = await planResponse.json();
  expect(plan.valid).toBe(true);

  await expect(page.locator('#apply-plan-output')).toContainText(/valid/i);
  expect(pageErrors).toEqual([]);
});
