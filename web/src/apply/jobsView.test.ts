import { describe, expect, it } from "vitest";
import {
	isSupersededRecoveryJob,
	liveApplyLastError,
	truncateApplyError,
} from "./jobsView";

describe("truncateApplyError", () => {
	it("leaves short messages intact", () => {
		expect(truncateApplyError("haproxy reload failed")).toBe(
			"haproxy reload failed",
		);
	});

	it("collapses and caps a giant recovery dump", () => {
		const giant = `runtime publication evidence transferred to a fresh full-convergence attempt\n${"x".repeat(400)}`;
		const out = truncateApplyError(giant, 80);
		expect(out.endsWith("…")).toBe(true);
		expect(out.length).toBe(80);
		expect(out).not.toContain("\n");
	});
});

describe("isSupersededRecoveryJob", () => {
	it("matches transferred and superseded recovery codes", () => {
		expect(
			isSupersededRecoveryJob({
				errorCode: "PUBLICATION_RECOVERY_TRANSFERRED",
			}),
		).toBe(true);
		expect(isSupersededRecoveryJob({ errorCode: "SUPERSEDED" })).toBe(true);
		expect(isSupersededRecoveryJob({ errorCode: "FIREWALL_PREPARE" })).toBe(
			false,
		);
	});
});

describe("liveApplyLastError", () => {
	const transferred = {
		code: "PUBLICATION_RECOVERY_TRANSFERRED",
		message: "runtime publication evidence transferred to a fresh attempt",
	};

	it("keeps a real live failure", () => {
		expect(
			liveApplyLastError(
				{
					state: "failed",
					desiredRevision: 3,
					appliedRevision: 1,
					lastError: { code: "FIREWALL_PREPARE", message: "ufw failed" },
				},
				[],
			),
		).toEqual({ code: "FIREWALL_PREPARE", message: "ufw failed" });
	});

	it("drops a transferred lastError when a later job succeeded", () => {
		expect(
			liveApplyLastError(
				{
					state: "failed",
					desiredRevision: 2,
					appliedRevision: 1,
					lastError: transferred,
				},
				[
					{ id: "ok", status: "succeeded", errorCode: undefined },
					{
						id: "old",
						status: "failed",
						errorCode: "PUBLICATION_RECOVERY_TRANSFERRED",
					},
				],
			),
		).toBeUndefined();
	});

	it("drops a transferred lastError when runtime has already caught up", () => {
		expect(
			liveApplyLastError(
				{
					state: "synced",
					desiredRevision: 4,
					appliedRevision: 4,
					lastError: transferred,
				},
				[
					{
						id: "old",
						status: "failed",
						errorCode: "PUBLICATION_RECOVERY_TRANSFERRED",
					},
				],
			),
		).toBeUndefined();
	});
});
