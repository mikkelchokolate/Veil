async function waitForSpa(page) {
  await page.waitForFunction(() => typeof window.__VEIL_BOOT === "function", null, {
    timeout: 15_000,
  });
  await page.evaluate(() => {
    window.__VEIL_BOOT();
  });
  await page.waitForFunction(() => window.__VEIL_READY === true, null, {
    timeout: 15_000,
  });
}

module.exports = { waitForSpa };
