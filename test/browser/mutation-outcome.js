// Shared Playwright assertion for Veil mutation envelopes (issue #677).
//
// An HTTP status < 300 is only half the mutation contract: committed
// mutations return a MutationOutcome body whose `success` reports whether
// the auto-apply converged. A seed that greens on status alone lets the rest
// of the scenario run on a false premise when the body says success=false.
const { expect } = require('@playwright/test');

// Non-terminal apply-job statuses (internal/apply Job.Terminal): staged,
// succeeded, failed, rolled_back and rollback_failed are terminal.
const NON_TERMINAL_JOB_STATUSES = new Set([
  'pending',
  'planning',
  'validating',
  'applying',
  'health_check',
  'recovery_pending',
  'rolling_back',
]);

// assertMutationOutcome(resp, opts) → parsed body.
//   label:             prefix for assertion messages.
//   allowApplyFailure: true only for the sandboxed main browser panel, whose
//                      privileged-helper alias CI detaches after startup so
//                      every apply honestly fails. Even then the response
//                      must carry the failed apply job as evidence — a bare
//                      success=false would hide a half-reported mutation.
async function assertMutationOutcome(resp, { label = 'mutation', allowApplyFailure = false } = {}) {
  const text = await resp.text();
  expect(resp.status(), `${label}: HTTP ${resp.status()} ${text}`).toBeLessThan(300);
  let body = {};
  try {
    body = JSON.parse(text);
  } catch {
    // Not every endpoint returns a JSON envelope; the status check stands.
  }
  if (body.success === false) {
    expect(
      allowApplyFailure,
      `${label}: committed but the apply failed (success=false) — ${text}`,
    ).toBe(true);
    expect(
      body.applyJob,
      `${label}: success=false without apply job evidence — ${text}`,
    ).toBeTruthy();
    expect(
      NON_TERMINAL_JOB_STATUSES.has(body.applyJob?.status),
      `${label}: apply job still in flight — wait for a terminal status instead of seeding on it — ${text}`,
    ).toBe(false);
  }
  return body;
}

module.exports = { assertMutationOutcome };
