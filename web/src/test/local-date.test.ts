import { dateInputToUnix, unixToDateInput } from "../lib/localDate";

describe("local date input conversion", () => {
	it("round-trips a calendar day through local midnight, not UTC parse", () => {
		const localMidnight = Math.floor(new Date(2026, 7, 21).getTime() / 1000);
		expect(dateInputToUnix("2026-08-21")).toBe(localMidnight);
		expect(unixToDateInput(localMidnight)).toBe("2026-08-21");
		expect(new Date("2026-08-21").getTime() / 1000).toBe(
			Date.parse("2026-08-21T00:00:00Z") / 1000,
		);
	});

	it("rejects non-date strings", () => {
		expect(Number.isNaN(dateInputToUnix("not-a-date"))).toBe(true);
		expect(Number.isNaN(dateInputToUnix(""))).toBe(true);
	});
});
