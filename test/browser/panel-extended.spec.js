// S8 extension: extended critical flows for the React SPA gate.
//
// Covers, against the REAL panel binary (no mocks):
//  1. two-binding create -> one-time credentials for BOTH inbounds, and proof
//     the plaintext never reaches localStorage/sessionStorage/URLs/Query cache
//  2. viewer RBAC (read-only UI + 403 on mutation)
//  3. optimistic-lock conflict -> 409 version_conflict
//  4. failed apply job -> retry produces a NEW job (no history rewrite)
//  5. subscription token lifecycle: issue -> fetch 200 -> revoke -> 404
//  6. backup create -> verify -> restore rolls state back
//  7. WebBasePath deployment: SPA boots and survives a hard refresh on a
//     deep link (gated on VEIL_BROWSER_BASE_URL_PATHED; CI runs a second
//     panel instance with --web-base-path for this)
const { test, expect } = require('@playwright/test');

const adminUsername = process.env.VEIL_BROWSER_USERNAME || 'browser-admin';
const adminPassword = process.env.VEIL_BROWSER_PASSWORD || 'Browser-E2E-Password-123!';
const apiToken = process.env.VEIL_BROWSER_API_TOKEN || 'browser-e2e-token';
const tokenHeaders = { 'X-Veil-Token': apiToken, 'Content-Type': 'application/json' };

async function login(page, username, password) {
  await page.goto('/');
  await expect(page.locator('#login-username')).toBeVisible({ timeout: 15_000 });
  await page.locator('#login-username').fill(username);
  await page.locator('#login-password').fill(password);
  await page.getByRole('button', { name: /^sign in$/i }).click();
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

async function seedInbound(request, name, port, enabled = false) {
  const resp = await request.post('/api/inbounds', {
    headers: tokenHeaders,
    data: { name, protocol: 'hysteria2', transport: 'udp', port, enabled },
  });
  expect(resp.status(), `inbound seed failed: ${resp.status()} ${await resp.text()}`).toBeLessThan(300);
  return resp;
}

async function createClientMutation(request, name, extra = {}) {
  const resp = await request.post('/api/v1/clients', {
    headers: tokenHeaders,
    data: { name, ...extra },
  });
  expect(resp.status(), `client create failed: ${resp.status()} ${await resp.text()}`).toBeLessThan(300);
  return resp.json();
}

async function createClientAPI(request, name, extra = {}) {
  return (await createClientMutation(request, name, extra)).client;
}

async function waitForApplyJob(request, jobID) {
  let job;
  await expect
    .poll(
      async () => {
        job = await (
          await request.get(`/api/apply/jobs/${jobID}`, { headers: tokenHeaders })
        ).json();
        return ['queued', 'running', 'applying', 'rolling_back'].includes(job.status)
          ? null
          : job.status;
      },
      { timeout: 30_000, intervals: [250, 500, 1000] },
    )
    .not.toBeNull();
  return job;
}

test.describe('Veil Panel — extended critical flows', () => {
  test('two-binding create shows one-time credentials for both inbounds and leaks no plaintext', async ({
    page,
    request,
  }) => {
    const stamp = Date.now();
    const inA = `e2e-inA-${stamp}`;
    const inB = `e2e-inB-${stamp}`;
    const port = 21000 + (stamp % 15000);
    await seedInbound(request, inA, port);
    await seedInbound(request, inB, port + 1);

    await login(page, adminUsername, adminPassword);
    await page.getByRole('link', { name: /clients/i }).first().click();
    const name = `e2e-two-${stamp}`;
    await page.getByRole('button', { name: /new client/i }).click();
    await page.locator('#nc-name').fill(name);
    await page.getByRole('button', { name: /^next$/i }).click();
    await page.getByRole('button', { name: /^next$/i }).click();
    await page.getByRole('checkbox', { name: new RegExp(inA) }).check();
    await page.getByRole('checkbox', { name: new RegExp(inB) }).check();
    await page.getByRole('button', { name: /^review$/i }).click();
    await page.getByRole('button', { name: /create client/i }).click();

    // One dialog, both inbounds, two distinct generated plaintexts.
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 15_000 });
    await expect(dialog).toContainText(inA);
    await expect(dialog).toContainText(inB);
    const plaintexts = await dialog.locator('code.mono').allTextContents();
    const secrets = plaintexts.map((t) => t.trim()).filter((t) => t.length >= 8);
    expect(secrets.length, `expected >= 2 plaintexts, got ${JSON.stringify(secrets)}`).toBeGreaterThanOrEqual(2);
    expect(new Set(secrets).size).toBe(secrets.length);

    // Plaintext must not persist anywhere client-side: storages, URL, Query
    // cache (window.__REACT_QUERY_DEVTOOLS__ is absent in prod; inspect the
    // dehydrated cache via the client's global hook if present) and network
    // resource names.
    const storageDump = await page.evaluate(() => {
      const dump = (s) => {
        const out = [];
        for (let i = 0; i < s.length; i++) {
          const k = s.key(i);
          out.push(k, s.getItem(k) || '');
        }
        return out.join('\n');
      };
      return `${dump(localStorage)}\n${dump(sessionStorage)}`;
    });
    const resourceNames = await page.evaluate(() =>
      performance.getEntriesByType('resource').map((e) => e.name).join('\n'),
    );
    const queryCacheDump = await page.evaluate(() => {
      // The SPA keeps its QueryClient module-private; anything reachable from
      // window would be a leak vector. Assert no secret materialised in the
      // rendered DOM outside the dialog either (checked below post-dismiss).
      return JSON.stringify(window.__REACT_QUERY_CLIENT__ || '');
    });
    for (const secret of secrets) {
      expect(storageDump, `plaintext in web storage`).not.toContain(secret);
      expect(page.url(), `plaintext in URL`).not.toContain(secret);
      expect(resourceNames, `plaintext in resource URL`).not.toContain(secret);
      expect(queryCacheDump, `plaintext in query cache`).not.toContain(secret);
    }

    await dialog.getByRole('button', { name: /^done$/i }).click();
    // After dismissal the plaintext must not linger in the DOM at all.
    const bodyText = await page.evaluate(() => document.body.innerText);
    for (const secret of secrets) {
      expect(bodyText, `plaintext lingered in DOM after dismiss`).not.toContain(secret);
    }

    await expect(page).toHaveURL(/\/clients/);
    await expect(page.getByRole('row', { name: new RegExp(name) })).toBeVisible({ timeout: 10_000 });

    // Both bindings committed server-side with credential metadata.
    const list = await (await request.get('/api/v1/clients', { headers: tokenHeaders })).json();
    const created = list.items.find((c) => c.name === name);
    expect(created, 'client visible via API').toBeTruthy();
    const detail = await (
      await request.get(`/api/v1/clients/${created.id}`, { headers: tokenHeaders })
    ).json();
    expect(detail.bindings?.length ?? detail.Bindings?.length).toBeGreaterThanOrEqual(2);
  });

  test('viewer role gets a read-only panel and 403 on mutation', async ({ page, request }) => {
    const stamp = Date.now();
    const viewerName = `e2e-viewer-${stamp}`;
    const viewerPassword = 'Viewer-E2E-Password-123!';
    const resp = await request.post('/api/users', {
      headers: tokenHeaders,
      data: { username: viewerName, password: viewerPassword, role: 'viewer', locale: 'en' },
    });
    expect(resp.status(), `viewer seed failed: ${resp.status()} ${await resp.text()}`).toBeLessThan(300);

    await login(page, viewerName, viewerPassword);
    await page.getByRole('link', { name: /clients/i }).first().click();
    await expect(page).toHaveURL(/\/clients/);
    // Reads render…
    await expect(page.getByRole('columnheader', { name: /name/i }).first()).toBeVisible({ timeout: 10_000 });
    // …but no mutation affordances.
    await expect(page.getByRole('button', { name: /new client/i })).toHaveCount(0);

    // Server-side enforcement: an in-page mutation with the viewer session +
    // valid CSRF token must be rejected with 403 (not a UI-side hiding).
    const status = await page.evaluate(async () => {
      const st = await fetch('/api/auth/status', { credentials: 'include' }).then((r) => r.json());
      const r = await fetch('/api/v1/clients', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': st.csrfToken },
        body: JSON.stringify({ name: 'rbac-probe-must-not-exist' }),
      });
      return r.status;
    });
    expect(status).toBe(403);
  });

  test('stale version update returns 409 version_conflict', async ({ request }) => {
    const stamp = Date.now();
    const created = await createClientAPI(request, `e2e-conflict-${stamp}`);
    expect(created.version, 'created client carries version').toBeGreaterThanOrEqual(1);

    // First update at the current version succeeds and bumps the version.
    const ok = await request.patch(`/api/v1/clients/${created.id}`, {
      headers: tokenHeaders,
      data: { name: created.name, notes: 'first write', version: created.version },
    });
    expect(ok.status(), `first update: ${ok.status()} ${await ok.text()}`).toBeLessThan(300);

    // Replaying the same stale version must conflict.
    const conflict = await request.patch(`/api/v1/clients/${created.id}`, {
      headers: tokenHeaders,
      data: { name: created.name, notes: 'stale write', version: created.version },
    });
    expect(conflict.status()).toBe(409);
    const body = await conflict.text();
    expect(body).toMatch(/version conflict/i);
  });

  test('failed apply job can be retried and produces a NEW job record', async ({ request }) => {
    // The browser-gate Panel uses a helper alias for startup recovery, then
    // the harness removes that alias. Every later apply therefore fails
    // honestly at the privileged boundary. A plain client mutation is enough
    // to enqueue one.
    const stamp = Date.now();
    await createClientAPI(request, `e2e-apply-${stamp}`);

    // Wait for the auto-apply job to reach a terminal state.
    let latest;
    await expect
      .poll(
        async () => {
          const jobs = await (
            await request.get('/api/apply/jobs', { headers: tokenHeaders })
          ).json();
          latest = jobs.items?.[0];
          return latest && !['queued', 'running', 'applying', 'rolling_back'].includes(latest.status)
            ? latest.status
            : null;
        },
        { timeout: 30_000, intervals: [500, 1000, 2000] },
      )
      .not.toBeNull();
    expect(latest.status, 'apply must fail after the browser helper alias is detached').toBe('failed');

    // Retry creates a NEW job for the same desired revision; the old record
    // is immutable history.
    const retry = await request.post(`/api/apply/jobs/${latest.id}/retry`, { headers: tokenHeaders });
    expect(retry.status(), `retry: ${retry.status()} ${await retry.text()}`).toBeLessThan(300);
    const retryBody = await retry.json();
    const newJob = retryBody.applyJob;
    expect(newJob, 'retry returns the new job').toBeTruthy();
    expect(newJob.id).not.toBe(latest.id);
    expect(newJob.desiredRevision).toBe(latest.desiredRevision);

    // The retried job also reaches a terminal state (the helper alias remains
    // detached), proving the retry actually executed rather than being parked
    // silently.
    await expect
      .poll(
        async () => {
          const job = await (
            await request.get(`/api/apply/jobs/${newJob.id}`, { headers: tokenHeaders })
          ).json();
          return ['queued', 'running', 'applying', 'rolling_back'].includes(job.status) ? null : job.status;
        },
        { timeout: 30_000, intervals: [500, 1000, 2000] },
      )
      .toBe('failed');

    // History intact: the original failed record is unchanged.
    const orig = await (
      await request.get(`/api/apply/jobs/${latest.id}`, { headers: tokenHeaders })
    ).json();
    expect(orig.status).toBe('failed');
  });

  test('subscription token lifecycle: issue, fetch, revoke, 404', async ({ browser, playwright }) => {
    const helperBase = process.env.VEIL_BROWSER_BACKUP_URL;
    test.skip(!helperBase, 'set VEIL_BROWSER_BACKUP_URL to a helper-backed panel');
    const context = await browser.newContext({ baseURL: helperBase });
    const page = await context.newPage();
    const request = await playwright.request.newContext({ baseURL: helperBase });
    try {
      const firewallUpdate = await request.put('/api/settings', {
        headers: tokenHeaders,
        data: {
          panelListen: '127.0.0.1:2098',
          mode: 'server',
          firewallManagement: false,
        },
      });
      const firewallText = await firewallUpdate.text();
      expect(
        firewallUpdate.status(),
        `disable firewall management: ${firewallUpdate.status()} ${firewallText}`,
      ).toBeLessThan(300);
      const firewallMutation = JSON.parse(firewallText);
      expect(firewallMutation.applyJob, 'settings mutation returns an apply job').toBeTruthy();
      const firewallJob = await waitForApplyJob(request, firewallMutation.applyJob.id);
      expect(firewallJob.status, `settings apply job: ${JSON.stringify(firewallJob)}`).toBe('succeeded');

      const stamp = Date.now();
      const creation = await createClientMutation(request, `e2e-token-${stamp}`);
      const created = creation.client;
      const createdApplyJob = creation.applyJob;
      expect(createdApplyJob, 'client mutation returns an apply job').toBeTruthy();

      // Public subscriptions render only immutable applied state. The
      // helper-backed panel can complete the real privileged apply; wait for
      // that exact mutation before issuing a public token.
      const applyJob = await waitForApplyJob(request, createdApplyJob.id);
      expect(applyJob.status, `client apply job: ${JSON.stringify(applyJob)}`).toBe('succeeded');

      await login(page, adminUsername, adminPassword);
      await page.goto(`/clients/${created.id}`);
      await page.getByRole('tab', { name: /^subscription$/i }).click();

      await page.getByPlaceholder(/label/i).fill(`e2e-label-${stamp}`);
      await page.getByRole('button', { name: /create token/i }).click();

      // Plaintext URL shown exactly once.
      const urlEl = page.locator('code.mono').filter({ hasText: '/s/' }).first();
      await expect(urlEl).toBeVisible({ timeout: 10_000 });
      const subURL = (await urlEl.textContent()).trim();
      expect(subURL).toContain('/s/');

      // The live token serves the applied subscription…
      const live = await request.get(subURL);
      expect(live.status(), `live token fetch: ${live.status()}`).toBe(200);

      // …until revoked.
      const tokenRow = page.getByRole('row').filter({ hasText: `e2e-label-${stamp}` });
      await expect(tokenRow.getByText(/^active$/i)).toBeVisible({ timeout: 10_000 });
      const revoked = page.waitForResponse((response) => {
        const path = new URL(response.url()).pathname;
        return (
          response.request().method() === 'DELETE' &&
          path.startsWith(`/api/v1/clients/${created.id}/tokens/`)
        );
      });
      await tokenRow.getByRole('button', { name: /^revoke$/i }).click();
      const revokedResponse = await revoked;
      expect(revokedResponse.status(), 'token revoke API must complete').toBe(200);
      await expect(tokenRow.locator('.badge-danger')).toBeVisible({ timeout: 10_000 });
      const dead = await request.get(subURL);
      expect(dead.status(), 'revoked token must 404 (oracle-safe)').toBe(404);
    } finally {
      await request.dispose();
      await context.close();
    }
  });

  test('backup create, verify, restore rolls state back', async ({ playwright }) => {
    // Backup operations are privileged (helper-only): the main gate panel has
    // no helper, so this flow targets a dedicated panel that runs with the
    // production layout (veil system user, /var/lib/veil state, root helper
    // on /run/veil/helper.sock). CI starts it; VEIL_BROWSER_BACKUP_URL points
    // at it.
    const backupBase = process.env.VEIL_BROWSER_BACKUP_URL;
    test.skip(!backupBase, 'set VEIL_BROWSER_BACKUP_URL to a helper-backed panel');
    const request = await playwright.request.newContext({ baseURL: backupBase });

    const stamp = Date.now();
    // Panel users provide a compact UI-level rollback probe. Archive v2 also
    // restores the normalized veil.db domain; real table coverage lives in
    // internal/backup to keep this browser flow focused.
    const beforeName = `e2e-backup-before-${stamp}`;
    const afterName = `e2e-backup-after-${stamp}`;
    const userShape = (username) => ({
      username,
      password: 'E2E-Backup-Password-123!',
      role: 'viewer',
      locale: 'en',
    });
    const createResp = await request.post('/api/users', {
      headers: tokenHeaders,
      data: userShape(beforeName),
    });
    expect(createResp.status(), `seed before-backup user: ${createResp.status()} ${await createResp.text()}`).toBeLessThan(300);

    // Create + auto-verify the archive.
    const created = await request.post('/api/backups', { headers: tokenHeaders, data: {} });
    expect(created.status(), `backup create: ${created.status()} ${await created.text()}`).toBeLessThan(300);
    const createdBody = await created.json();
    const archiveName = createdBody.archive?.name;
    expect(archiveName, 'archive name returned').toBeTruthy();

    // Explicit verify passes.
    const verify = await request.post(`/api/backups/${archiveName}/verify`, {
      headers: tokenHeaders,
      data: {},
    });
    expect(verify.status(), `verify: ${verify.status()} ${await verify.text()}`).toBe(200);

    // A post-backup mutation…
    const afterResp = await request.post('/api/users', {
      headers: tokenHeaders,
      data: userShape(afterName),
    });
    expect(afterResp.status()).toBeLessThan(300);

    // …is rolled back by the restore.
    const restore = await request.post(`/api/backups/${archiveName}/restore`, {
      headers: tokenHeaders,
      data: { confirm: true },
    });
    expect(restore.status(), `restore: ${restore.status()} ${await restore.text()}`).toBe(202);
    const restoreJob = await restore.json();
    expect(restoreJob.id).toBeTruthy();

    // The restore job reaches succeeded…
    await expect
      .poll(
        async () => {
          const job = await (
            await request.get(`/api/backup-restore-jobs/${restoreJob.id}`, { headers: tokenHeaders })
          ).json();
          return ['queued', 'running'].includes(job.status) ? null : job.status;
        },
        { timeout: 30_000, intervals: [500, 1000, 2000] },
      )
      .toBe('succeeded');

    // …and the state actually rolled back: pre-backup user present,
    // post-backup user gone.
    await expect
      .poll(
        async () => {
          const list = await (
            await request.get('/api/users', { headers: tokenHeaders })
          ).json();
          const names = (Array.isArray(list) ? list : list.items || []).map((u) => u.username);
          return names.includes(beforeName) && !names.includes(afterName) ? 'rolled-back' : null;
        },
        { timeout: 30_000, intervals: [1000, 2000, 3000] },
      )
      .toBe('rolled-back');
    await request.dispose();
  });

  test('panel behind a WebBasePath boots and survives hard refresh on a deep link', async ({
    page,
    browser,
  }) => {
    const pathBase = process.env.VEIL_BROWSER_BASE_URL_PATHED;
    test.skip(!pathBase, 'set VEIL_BROWSER_BASE_URL_PATHED to a panel started with --web-base-path');
    const base = new URL(pathBase);
    const prefix = base.pathname.replace(/\/$/, '');

    // The root no longer serves the SPA.
    const root = await page.request.get(`${base.origin}/`);
    expect(root.status()).toBeGreaterThanOrEqual(404);

    // The SPA boots under the prefix (assets resolve relative to it).
    // Absolute URLs only: page.goto with a relative path would resolve
    // against the config baseURL (the unpathed panel).
    await page.goto(`${base.origin}${prefix}/`);
    await expect(page.locator('#login-username')).toBeVisible({ timeout: 15_000 });

    // Hard refresh on a DEEP link: the server must serve the SPA shell, not
    // 404, so the router can take over.
    await page.goto(`${base.origin}${prefix}/clients`);
    await expect(page.locator('#login-username')).toBeVisible({ timeout: 15_000 });

    // Full auth flow under the prefix.
    await page.locator('#login-username').fill(adminUsername);
    await page.locator('#login-password').fill(adminPassword);
    await page.getByRole('button', { name: /^sign in$/i }).click();
    await expect(page.getByRole('link', { name: /clients/i }).first()).toBeVisible({ timeout: 20_000 });
    await page.getByRole('link', { name: /clients/i }).first().click();
    await expect(page).toHaveURL(new RegExp(`${prefix}/clients`));
    await expect(page.getByRole('columnheader', { name: /name/i }).first()).toBeVisible({ timeout: 10_000 });
  });
});
