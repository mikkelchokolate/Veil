async function waitForSpa(page) {
  await page.locator("#login-username").click();
  await page.waitForFunction(() => window.__VEIL_READY === true, null, {
    timeout: 15_000,
  });
}

module.exports = { waitForSpa };
