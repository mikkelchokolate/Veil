import { apiFetch } from "./fetcher";
import type { VersionResponse } from "./generated/models";

/** Staging downloads, verifies, and installs the latest release before 202. */
export const PANEL_UPDATE_TIMEOUT_MS = 180_000;
export const PANEL_UPDATE_POLL_DELAY_MS = 3_000;
export const PANEL_UPDATE_POLL_INTERVAL_MS = 2_000;
export const PANEL_UPDATE_POLL_MAX_ATTEMPTS = 20;

export class PanelRestartTimeoutError extends Error {
	constructor() {
		super("panel restart timed out");
		this.name = "PanelRestartTimeoutError";
	}
}

export class PanelUpdateFailedError extends Error {
	constructor(message: string) {
		super(message);
		this.name = "PanelUpdateFailedError";
	}
}

export type PanelUpdateResponse = {
	jobId?: string;
	status?: string;
	staged?: boolean;
	installed?: boolean;
	version?: string;
	message?: string;
};

export type PanelUpdateJob = {
	id: string;
	version?: string;
	status: string;
	error?: string;
};

export function postPanelUpdate(): Promise<PanelUpdateResponse> {
	return apiFetch<PanelUpdateResponse>("/api/version/update", {
		method: "POST",
		timeoutMs: PANEL_UPDATE_TIMEOUT_MS,
	});
}

/** Strip an optional " (commit)" suffix so tag comparison survives rebuilds. */
export function panelVersionIdentity(version: string): string {
	return version.replace(/\s*\([^)]*\)\s*$/, "").trim();
}

export type WaitForPanelVersionOptions = {
	delayMs?: number;
	intervalMs?: number;
	maxAttempts?: number;
	previousVersion?: string;
	expectedVersion?: string;
	jobId?: string;
	fetchVersion?: () => Promise<VersionResponse>;
	fetchJob?: (jobId: string) => Promise<PanelUpdateJob>;
	sleep?: (ms: number) => Promise<void>;
	onAttempt?: (attempt: number, max: number) => void;
};

function defaultFetchVersion(): Promise<VersionResponse> {
	return apiFetch<VersionResponse>("/api/version", {
		timeoutMs: 5_000,
		attempts: 1,
	});
}

function defaultFetchJob(jobId: string): Promise<PanelUpdateJob> {
	return apiFetch<PanelUpdateJob>(
		`/api/version/update/jobs/${encodeURIComponent(jobId)}`,
		{
			timeoutMs: 5_000,
			attempts: 1,
		},
	);
}

/** Wait until GET /api/version is a different binary (or the update job is
 * terminal). A 200 from the still-running old process is not success. */
export async function waitForPanelVersion(
	options: WaitForPanelVersionOptions = {},
): Promise<VersionResponse> {
	const delayMs = options.delayMs ?? PANEL_UPDATE_POLL_DELAY_MS;
	const intervalMs = options.intervalMs ?? PANEL_UPDATE_POLL_INTERVAL_MS;
	const maxAttempts = options.maxAttempts ?? PANEL_UPDATE_POLL_MAX_ATTEMPTS;
	const sleep =
		options.sleep ??
		((ms) => new Promise((resolve) => setTimeout(resolve, ms)));
	const fetchVersion = options.fetchVersion ?? defaultFetchVersion;
	const fetchJob = options.fetchJob ?? defaultFetchJob;
	const previousIdentity = options.previousVersion
		? panelVersionIdentity(options.previousVersion)
		: undefined;
	// When the caller staged a specific target, only that version identity is
	// success — a version change to an unexpected binary is not.
	const expectedIdentity = options.expectedVersion
		? panelVersionIdentity(options.expectedVersion)
		: undefined;
	const isExpectedBinary = (current: VersionResponse): boolean => {
		const currentIdentity = panelVersionIdentity(current.version);
		if (expectedIdentity) return currentIdentity === expectedIdentity;
		return !previousIdentity || currentIdentity !== previousIdentity;
	};

	await sleep(delayMs);
	for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
		options.onAttempt?.(attempt, maxAttempts);
		try {
			if (options.jobId) {
				try {
					const job = await fetchJob(options.jobId);
					if (job.status === "failed") {
						throw new PanelUpdateFailedError(
							job.error?.trim() || "panel update failed",
						);
					}
					if (job.status === "succeeded") {
						const current = await fetchVersion();
						if (!isExpectedBinary(current)) {
							throw new PanelUpdateFailedError(
								`panel restarted at ${current.version}, expected ${options.expectedVersion}`,
							);
						}
						return current;
					}
				} catch (error) {
					if (error instanceof PanelUpdateFailedError) throw error;
				}
			}
			const current = await fetchVersion();
			if (isExpectedBinary(current)) {
				return current;
			}
		} catch (error) {
			if (error instanceof PanelUpdateFailedError) throw error;
			if (attempt === maxAttempts) {
				throw new PanelRestartTimeoutError();
			}
			await sleep(intervalMs);
			continue;
		}
		if (attempt === maxAttempts) {
			throw new PanelRestartTimeoutError();
		}
		await sleep(intervalMs);
	}
	throw new PanelRestartTimeoutError();
}

/** Hashed SPA assets change with the binary; index.html is no-store. */
export function reloadPanel(): void {
	window.location.reload();
}
