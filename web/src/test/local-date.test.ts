import { dateInputToUnix, unixToDateInput } from "../lib/localDate";

describe("local date input conversion", () => {
	it("round-trips a calendar day through local end of day, not UTC parse", () => {
		const localEnd = Math.floor((new Date(2026, 7, 22).getTime() - 1) / 1000);
		expect(dateInputToUnix("2026-08-21")).toBe(localEnd);
		expect(unixToDateInput(localEnd)).toBe("2026-08-21");
		expect(new Date("2026-08-21").getTime() / 1000).toBe(
			Date.parse("2026-08-21T00:00:00Z") / 1000,
		);
	});

	it("keeps today's expiry in the future at local noon", () => {
		const now = new Date();
		const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
		expect(dateInputToUnix(today)).toBeGreaterThan(Date.now() / 1000);
	});

	it("rejects non-date strings", () => {
		expect(Number.isNaN(dateInputToUnix("not-a-date"))).toBe(true);
		expect(Number.isNaN(dateInputToUnix(""))).toBe(true);
	});
});
