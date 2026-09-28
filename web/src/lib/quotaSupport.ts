/** Quota-enforcement support for one bound inbound.
 *
 * The server is the source of truth in both directions: it advertises
 * `BindingCapability.quotaEnforcement` on every persisted binding view and
 * re-validates quota writes itself (currently only hysteria2 enforces
 * quota). The UI verdict returned here is advisory — it drives the inline
 * hint and the pre-submit check, never the final decision.
 *
 * Precedence:
 *  1. `capability.quotaEnforcement` whenever the binding carries a verdict.
 *  2. Fallback to the protocol-name check ONLY when no capability verdict
 *     exists — ClientNewPage binding drafts are not persisted yet (the
 *     /api/inbounds catalog exposes protocol+enabled, not capabilities) and
 *     stale/partial payloads may omit the field. Keep this in sync with the
 *     backend's protocolQuotaEnforcement.
 *  3. `null` = cannot decide (unknown inbound, no capability) — callers
 *     must NOT treat this as "unsupported"; server-side validation makes
 *     the final call on submit.
 */
export function quotaEnforcementVerdict(
	capability: { quotaEnforcement?: boolean } | null | undefined,
	protocol: string | null | undefined,
): boolean | null {
	if (capability?.quotaEnforcement != null) {
		return capability.quotaEnforcement;
	}
	if (protocol == null) {
		return null;
	}
	return protocol === "hysteria2";
}
