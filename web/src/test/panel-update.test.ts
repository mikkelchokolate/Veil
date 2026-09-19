import { describe, expect, it, vi } from "vitest";
import {
	PanelRestartTimeoutError,
	PanelUpdateFailedError,
	panelVersionIdentity,
	waitForPanelVersion,
} from "../api/panelUpdate";

describe("panelVersionIdentity", () => {
	it("ignores an optional commit suffix", () => {
		expect(panelVersionIdentity("v0.6.4 (abc1234)")).toBe("v0.6.4");
		expect(panelVersionIdentity("v0.6.4")).toBe("v0.6.4");
	});
});

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

	it("keeps waiting while GET /api/version still reports the pre-update binary", async () => {
		const fetchVersion = vi
			.fn()
			.mockResolvedValueOnce({
				version: "v0.6.3",
				runtime: "linux/amd64",
				name: "Veil",
			})
			.mockResolvedValueOnce({
				version: "v0.6.4 (deadbeef)",
				runtime: "linux/amd64",
				name: "Veil",
			});

		const result = await waitForPanelVersion({
			delayMs: 0,
			intervalMs: 0,
			maxAttempts: 3,
			previousVersion: "v0.6.3 (abc)",
			fetchVersion,
			sleep: async () => undefined,
		});

		expect(result.version).toBe("v0.6.4 (deadbeef)");
		expect(fetchVersion).toHaveBeenCalledTimes(2);
	});

	it("does not treat a 200 from the old binary as success when a job is still pending", async () => {
		const fetchVersion = vi
			.fn()
			.mockResolvedValueOnce({
				version: "v0.6.3",
				runtime: "linux/amd64",
				name: "Veil",
			})
			.mockResolvedValueOnce({
				version: "v0.6.4",
				runtime: "linux/amd64",
				name: "Veil",
			});
		const fetchJob = vi
			.fn()
			.mockResolvedValueOnce({
				id: "job-1",
				status: "restart_pending",
				version: "v0.6.4",
			})
			.mockResolvedValueOnce({
				id: "job-1",
				status: "restarting",
				version: "v0.6.4",
			});

		const result = await waitForPanelVersion({
			delayMs: 0,
			intervalMs: 0,
			maxAttempts: 3,
			previousVersion: "v0.6.3",
			jobId: "job-1",
			fetchVersion,
			fetchJob,
			sleep: async () => undefined,
		});

		expect(result.version).toBe("v0.6.4");
		expect(fetchJob).toHaveBeenCalled();
	});

	it("reloads when the update job reports succeeded", async () => {
		const fetchVersion = vi.fn().mockResolvedValue({
			version: "v0.6.4",
			runtime: "linux/amd64",
			name: "Veil",
		});
		const fetchJob = vi.fn().mockResolvedValue({
			id: "job-1",
			status: "succeeded",
			version: "v0.6.4",
		});

		const result = await waitForPanelVersion({
			delayMs: 0,
			intervalMs: 0,
			maxAttempts: 2,
			previousVersion: "v0.6.3",
			jobId: "job-1",
			fetchVersion,
			fetchJob,
			sleep: async () => undefined,
		});

		expect(result.version).toBe("v0.6.4");
		expect(fetchJob).toHaveBeenCalledTimes(1);
	});

	it("fails without waiting for a version change when the job is failed", async () => {
		await expect(
			waitForPanelVersion({
				delayMs: 0,
				intervalMs: 0,
				maxAttempts: 3,
				previousVersion: "v0.6.3",
				jobId: "job-1",
				fetchVersion: async () => ({
					version: "v0.6.3",
					runtime: "linux/amd64",
					name: "Veil",
				}),
				fetchJob: async () => ({
					id: "job-1",
					status: "failed",
					error: "helper refused restart",
				}),
				sleep: async () => undefined,
			}),
		).rejects.toSatisfy(
			(error: unknown) =>
				error instanceof PanelUpdateFailedError &&
				error.message === "helper refused restart",
		);
	});

	// Regression for #345: expectedVersion is the staged target — a drift to
	// any other version identity must not be reported as success.
	it("waits for the expected version instead of any version change", async () => {
		const fetchVersion = vi
			.fn()
			.mockResolvedValueOnce({
				version: "v0.6.9",
				runtime: "linux/amd64",
				name: "Veil",
			})
			.mockResolvedValueOnce({
				version: "v0.6.4 (deadbeef)",
				runtime: "linux/amd64",
				name: "Veil",
			});

		const result = await waitForPanelVersion({
			delayMs: 0,
			intervalMs: 0,
			maxAttempts: 3,
			previousVersion: "v0.6.3",
			expectedVersion: "v0.6.4",
			fetchVersion,
			sleep: async () => undefined,
		});

		expect(result.version).toBe("v0.6.4 (deadbeef)");
		expect(fetchVersion).toHaveBeenCalledTimes(2);
	});

	it("times out when the version drifts to an unexpected identity", async () => {
		await expect(
			waitForPanelVersion({
				delayMs: 0,
				intervalMs: 0,
				maxAttempts: 2,
				previousVersion: "v0.6.3",
				expectedVersion: "v0.6.4",
				fetchVersion: async () => ({
					version: "v0.6.9",
					runtime: "linux/amd64",
					name: "Veil",
				}),
				sleep: async () => undefined,
			}),
		).rejects.toBeInstanceOf(PanelRestartTimeoutError);
	});

	it("fails a succeeded job whose running binary is not the expected version", async () => {
		await expect(
			waitForPanelVersion({
				delayMs: 0,
				intervalMs: 0,
				maxAttempts: 2,
				previousVersion: "v0.6.3",
				expectedVersion: "v0.6.4",
				jobId: "job-1",
				fetchVersion: async () => ({
					version: "v0.6.9",
					runtime: "linux/amd64",
					name: "Veil",
				}),
				fetchJob: async () => ({
					id: "job-1",
					status: "succeeded",
					version: "v0.6.4",
				}),
				sleep: async () => undefined,
			}),
		).rejects.toSatisfy(
			(error: unknown) =>
				error instanceof PanelUpdateFailedError &&
				error.message.includes("expected v0.6.4"),
		);
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

	it("times out if the version never changes", async () => {
		await expect(
			waitForPanelVersion({
				delayMs: 0,
				intervalMs: 0,
				maxAttempts: 2,
				previousVersion: "v0.6.3",
				fetchVersion: async () => ({
					version: "v0.6.3",
					runtime: "linux/amd64",
					name: "Veil",
				}),
				sleep: async () => undefined,
			}),
		).rejects.toBeInstanceOf(PanelRestartTimeoutError);
	});
});
