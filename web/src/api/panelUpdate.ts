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

export function postPanelUpdate(): Promise<unknown> {
	return apiFetch("/api/version/update", {
		method: "POST",
		timeoutMs: PANEL_UPDATE_TIMEOUT_MS,
	});
}

export type WaitForPanelVersionOptions = {
	delayMs?: number;
	intervalMs?: number;
	maxAttempts?: number;
	fetchVersion?: () => Promise<VersionResponse>;
	sleep?: (ms: number) => Promise<void>;
	onAttempt?: (attempt: number, max: number) => void;
};

/** Wait for GET /api/version after the panel restarts onto the staged binary. */
export async function waitForPanelVersion(
	options: WaitForPanelVersionOptions = {},
): Promise<VersionResponse> {
	const delayMs = options.delayMs ?? PANEL_UPDATE_POLL_DELAY_MS;
	const intervalMs = options.intervalMs ?? PANEL_UPDATE_POLL_INTERVAL_MS;
	const maxAttempts = options.maxAttempts ?? PANEL_UPDATE_POLL_MAX_ATTEMPTS;
	const sleep =
		options.sleep ??
		((ms) => new Promise((resolve) => setTimeout(resolve, ms)));
	const fetchVersion =
		options.fetchVersion ??
		(() =>
			apiFetch<VersionResponse>("/api/version", {
				timeoutMs: 5_000,
				attempts: 1,
			}));

	await sleep(delayMs);
	for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
		options.onAttempt?.(attempt, maxAttempts);
		try {
			return await fetchVersion();
		} catch {
			if (attempt === maxAttempts) {
				throw new PanelRestartTimeoutError();
			}
			await sleep(intervalMs);
		}
	}
	throw new PanelRestartTimeoutError();
}

/** Hashed SPA assets change with the binary; index.html is no-store. */
export function reloadPanel(): void {
	window.location.reload();
}
