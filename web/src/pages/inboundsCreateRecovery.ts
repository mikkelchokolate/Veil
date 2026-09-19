import {
	ApiError,
	type ApiFetchOptions,
	apiFetch,
	TimeoutError,
} from "../api/fetcher";
import type { ApplyJob, Inbound } from "../api/generated/models";

/** Feedback extracted from a create response or a reconciled commit. */
export interface CreateOutcome {
	revision?: { desired?: number; applied?: number; state?: string };
	applyJob?: { id?: string; status?: string };
	success?: boolean;
	/** reconciled marks an outcome recovered after a lost response (timeout). */
	reconciled?: boolean;
}

// idempotencyInFlight reports whether the server still holds the original
// request under our Idempotency-Key (the durable store answers 409/408 while
// the committed mutation keeps running past the browser deadline).
function idempotencyInFlight(error: unknown): boolean {
	return (
		error instanceof ApiError &&
		(error.status === 408 ||
			(error.status === 409 &&
				typeof error.message === "string" &&
				error.message.includes("idempotent operation")))
	);
}

// reconcileCommittedCreate confirms the unknown-outcome create by looking up
// the committed object by name and attaching the latest apply job. It returns
// null when the inbound was never committed (a genuine pre-commit rejection).
async function reconcileCommittedCreate(
	name: string,
): Promise<CreateOutcome | null> {
	let list: Inbound[];
	try {
		list = await apiFetch<Inbound[]>("/api/inbounds");
	} catch {
		return null;
	}
	if (!list.some((ib) => ib.name === name)) return null;
	let applyJob: CreateOutcome["applyJob"];
	try {
		const jobs = await apiFetch<{ items?: ApplyJob[] }>("/api/apply/jobs");
		const latest = jobs.items?.[0];
		if (latest?.id) {
			applyJob = { id: latest.id, status: latest.status };
		}
	} catch {
		// The job id is best-effort; the committed inbound is the evidence.
	}
	// Match the server contract (management_operational_routes.go): with an
	// attached job the object is only honestly "saved and live" when the job
	// succeeded; running/failed/recovery_pending must not paint green.
	if (applyJob) {
		return {
			success: applyJob.status === "succeeded",
			reconciled: true,
			applyJob,
		};
	}
	return { success: true, reconciled: true };
}

// createInbound posts a new inbound and resolves an unknown response outcome.
// A committed create keeps applying on the panel lifecycle context even when
// the browser aborts the request at the mutation deadline. The
// Idempotency-Key makes a retry wait for and replay that in-flight request
// instead of inserting a duplicate; if the replay cannot produce a response
// either, the committed object is confirmed by name instead of leaving the
// operator with a retryable Create form that hits duplicate-name.
export async function createInbound(
	body: unknown,
	name: string,
): Promise<CreateOutcome> {
	const init: ApiFetchOptions = {
		method: "POST",
		headers: { "Idempotency-Key": crypto.randomUUID() },
		body: JSON.stringify(body),
	};
	try {
		return await apiFetch<CreateOutcome>("/api/inbounds", init);
	} catch (first) {
		if (!(first instanceof TimeoutError)) throw first;
		try {
			return await apiFetch<CreateOutcome>("/api/inbounds", init);
		} catch (second) {
			if (second instanceof ApiError && !idempotencyInFlight(second)) {
				// A replayed 4xx/5xx is the authoritative outcome of the first
				// request: the create was rejected before commit, so no committed
				// object can exist to reconcile.
				throw second;
			}
			const reconciled = await reconcileCommittedCreate(name);
			if (reconciled) return reconciled;
			throw second;
		}
	}
}
