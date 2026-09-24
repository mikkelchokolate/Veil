// #349: every primary nav entry must at least smoke-open — a bad route,
// missing lazy chunk, or crash-on-mount must fail browser-e2e rather than ship
// silently. This spec only navigates and asserts a stable landmark + an empty
// pageerror list; it performs no mutations.
const { test, expect } = require('@playwright/test');
const { waitForSpa } = require('./spa-boot');

const adminUsername = process.env.VEIL_BROWSER_USERNAME || 'browser-admin';
const adminPassword = process.env.VEIL_BROWSER_PASSWORD || 'Browser-E2E-Password-123!';

// Mirrors NAV_ENTRIES in web/src/shell/nav.ts. Each entry pairs the nav link
// label (en catalog nav.*) with the stable page heading (<h2> *.title).
// /clients, /inbounds and /traffic already have dedicated open-tests in
// panel.spec.js, so they are intentionally absent here.
const NAV_SMOKE = [
  { link: /^overview$/i, url: /\/$/, heading: /^overview$/i },
  { link: /^routing$/i, url: /\/routing$/, heading: /^routing rules$/i },
  { link: /^warp$/i, url: /\/warp$/, heading: /^warp outbound$/i },
  { link: /^system$/i, url: /\/system$/, heading: /^system$/i },
  { link: /^backups$/i, url: /\/backups$/, heading: /^backups$/i },
  // This spec logs in as admin, so /users must render the management
  // heading exactly — no soft-OR against the viewer title (#849). A viewer
  // run, if one is added, asserts /^users$/i instead.
  { link: /^users$/i, url: /\/users$/, heading: /^panel users$/i },
  { link: /^settings$/i, url: /\/settings$/, heading: /^settings$/i },
  { link: /^apply$/i, url: /\/apply$/, heading: /^apply state$/i },
];

test.describe('Veil Panel — nav smoke', () => {
  test('every nav entry mounts its page without runtime errors', async ({ page }) => {
    const pageErrors = [];
    page.on('pageerror', (error) => pageErrors.push(error.message));

    await page.goto('/');
    await expect(page.locator('#login-username')).toBeVisible({ timeout: 15_000 });
    await waitForSpa(page);
    await page.locator('#login-username').fill(adminUsername);
    await page.locator('#login-password').fill(adminPassword);
    await page.getByRole('button', { name: /^sign in$/i }).click();
    await expect(page.getByRole('link', { name: /clients/i }).first()).toBeVisible({
      timeout: 20_000,
    });

    for (const entry of NAV_SMOKE) {
      await page.getByRole('link', { name: entry.link }).first().click();
      await expect(page).toHaveURL(entry.url);
      await expect(
        page.getByRole('heading', { name: entry.heading }).first(),
        `nav entry ${entry.link} did not render its page heading`,
      ).toBeVisible({ timeout: 10_000 });
    }

    expect(pageErrors, `pageerror fired during nav smoke: ${pageErrors.join(' | ')}`).toEqual([]);
  });
});
