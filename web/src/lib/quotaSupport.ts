/** Quota-enforcement support for one bound inbound.
 *
 * The server is the source of truth in both directions: it advertises
 * `BindingCapability.quotaEnforcement` on every persisted binding view,
 * `ProtocolInfo.quotaEnforcement` on the /api/protocols catalog, and
 * re-validates quota writes itself. The UI verdict returned here is
 * advisory — it drives the inline hint and the pre-submit check, never the
 * final decision.
 *
 * Precedence:
 *  1. `capability.quotaEnforcement` whenever the binding carries a verdict.
 *  2. `protocolSupport` — the /api/protocols catalog verdict for the
 *     binding's protocol (used by ClientNewPage drafts that are not
 *     persisted yet, and by stale/partial payloads).
 *  3. `null` = cannot decide (unknown inbound, catalog not loaded) —
 *     callers must NOT treat this as "unsupported"; server-side validation
 *     makes the final call on submit.
 */
export function quotaEnforcementVerdict(
	capability: { quotaEnforcement?: boolean } | null | undefined,
	protocolSupport: boolean | null | undefined,
): boolean | null {
	if (capability?.quotaEnforcement != null) {
		return capability.quotaEnforcement;
	}
	return protocolSupport ?? null;
}
