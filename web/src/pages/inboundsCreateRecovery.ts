import {
	ApiError,
	type ApiFetchOptions,
	apiFetch,
	TimeoutError,
} from "../api/fetcher";
import type {
	ApplyJob,
	ApplyStateResponse,
	Inbound,
} from "../api/generated/models";

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

// correlatedApplyJob picks the job that gates the CURRENT desired revision —
// the one the apply state names (active / last succeeded / last failed) or
// the one targeting that revision. /api/apply/jobs is global and newest-first
// (handleApplyJobs), so a bare items[0] can belong to a concurrent mutation
// and must never decide this create's outcome (#700).
function correlatedApplyJob(
	state: ApplyStateResponse,
	items: ApplyJob[],
): CreateOutcome["applyJob"] {
	const named = new Set(
		[
			state.activeJobId,
			state.lastSuccessfulJobId,
			state.lastFailedJobId,
		].filter((id): id is string => Boolean(id)),
	);
	const job =
		items.find((entry) => entry.id && named.has(entry.id)) ??
		items.find((entry) => entry.desiredRevision === state.desiredRevision);
	return job?.id ? { id: job.id, status: job.status } : undefined;
}

// reconcileCommittedCreate confirms the unknown-outcome create by looking up
// the committed object by name, then proves convergence from the system apply
// state — only "synced" means the desired revision containing this inbound is
// live on the runtime. An empty/failed jobs fetch or an absent apply job is
// saved-but-unproven, never a green "saved and live" (#689). Returns null when
// the inbound was never committed (a genuine pre-commit rejection).
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
	let state: ApplyStateResponse | undefined;
	try {
		state = await apiFetch<ApplyStateResponse>("/api/apply/state");
	} catch {
		// No server signal — committed but unproven; report success:false.
	}
	let items: ApplyJob[] = [];
	try {
		const jobs = await apiFetch<{ items?: ApplyJob[] }>("/api/apply/jobs");
		items = jobs.items ?? [];
	} catch {
		// Job detail is best-effort evidence; the state view alone decides.
	}
	const applyJob = state ? correlatedApplyJob(state, items) : undefined;
	return {
		// Match the server contract (management_operational_routes.go): only a
		// synced state is explicit evidence the runtime converged — everything
		// else (pending/failed/degraded/untracked or no evidence at all) is
		// honest "saved but not live".
		success: state?.state === "synced",
		reconciled: true,
		...(state
			? {
					revision: {
						desired: state.desiredRevision,
						applied: state.appliedRevision,
						state: state.state,
					},
				}
			: {}),
		...(applyJob ? { applyJob } : {}),
	};
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
