import type { ApplyErrorView, ApplyState } from "./ApplyStatusIndicator";

type JobView = {
	id?: string;
	status: string;
	errorCode?: string;
};

/** Recovery rewrites the previous job with this code and starts a new apply. */
export const PUBLICATION_RECOVERY_TRANSFERRED =
	"PUBLICATION_RECOVERY_TRANSFERRED";

const SUPERSEDED_RECOVERY_CODES = new Set([
	PUBLICATION_RECOVERY_TRANSFERRED,
	"SUPERSEDED",
]);

export const APPLY_ERROR_CELL_MAX = 140;

export function isRecoveryTransferCode(code?: string): boolean {
	return code != null && SUPERSEDED_RECOVERY_CODES.has(code);
}

export function isSupersededRecoveryJob(
	job: Pick<JobView, "errorCode">,
): boolean {
	return isRecoveryTransferCode(job.errorCode);
}

export function truncateApplyError(
	message: string,
	max = APPLY_ERROR_CELL_MAX,
): string {
	const compact = message.replace(/\s+/g, " ").trim();
	if (compact.length <= max) return compact;
	return `${compact.slice(0, Math.max(0, max - 1))}…`;
}

/** lastError from a transferred recovery job is not the live apply outcome
 * once a later job has already succeeded (or runtime has caught up). */
export function liveApplyLastError(
	state: Pick<
		ApplyState,
		| "state"
		| "desiredRevision"
		| "appliedRevision"
		| "lastError"
		| "lastSuccessfulJobId"
		| "lastFailedJobId"
	>,
	jobs: JobView[],
): ApplyErrorView | undefined {
	const err = state.lastError;
	if (!err?.message) return undefined;
	if (!isRecoveryTransferCode(err.code)) return err;

	const inSync = state.desiredRevision === state.appliedRevision;
	// Only the OpenAPI enum value "synced" counts as in-sync (#674): a bogus
	// or legacy value like "applied" is not a settled signal and must not
	// silence a transferred recovery error.
	const settled = inSync && state.state === "synced";
	if (settled) return undefined;
	if (jobs.some((job) => job.status === "succeeded")) return undefined;
	if (
		state.lastSuccessfulJobId &&
		state.lastFailedJobId &&
		state.lastSuccessfulJobId !== state.lastFailedJobId &&
		inSync
	) {
		return undefined;
	}
	return err;
}
