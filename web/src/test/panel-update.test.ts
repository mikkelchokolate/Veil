import { describe, expect, it, vi } from "vitest";
import {
	PanelRestartTimeoutError,
	waitForPanelVersion,
} from "../api/panelUpdate";

describe("waitForPanelVersion", () => {
	it("returns the first successful version after downtime", async () => {
		const fetchVersion = vi
			.fn()
			.mockRejectedValueOnce(new Error("down"))
			.mockResolvedValueOnce({
				version: "v0.6.4",
				runtime: "linux/amd64",
				name: "Veil",
			});
		const attempts: number[] = [];

		const result = await waitForPanelVersion({
			delayMs: 0,
			intervalMs: 0,
			maxAttempts: 3,
			fetchVersion,
			sleep: async () => undefined,
			onAttempt: (attempt) => attempts.push(attempt),
		});

		expect(result.version).toBe("v0.6.4");
		expect(fetchVersion).toHaveBeenCalledTimes(2);
		expect(attempts).toEqual([1, 2]);
	});

	it("times out after the last failed poll", async () => {
		await expect(
			waitForPanelVersion({
				delayMs: 0,
				intervalMs: 0,
				maxAttempts: 2,
				fetchVersion: async () => {
					throw new Error("down");
				},
				sleep: async () => undefined,
			}),
		).rejects.toBeInstanceOf(PanelRestartTimeoutError);
	});
});
