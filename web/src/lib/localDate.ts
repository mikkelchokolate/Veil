/** Calendar-day helpers for `<input type="date">`.

HTML date values are local calendar days (`YYYY-MM-DD`), not UTC instants.
`Date#toISOString` and `new Date("YYYY-MM-DD")` are UTC and shift the day
west of UTC. */

function pad2(n: number): string {
	return String(n).padStart(2, "0");
}

export function unixToDateInput(ts: number): string {
	const d = new Date(ts * 1000);
	return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}

/** Inclusive end of the local calendar day (23:59:59 local). The date picker
 * has no time control, so choosing a day must keep access through that day. */
export function dateInputToUnix(value: string): number {
	const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value.trim());
	if (!match) return Number.NaN;
	const year = Number(match[1]);
	const month = Number(match[2]);
	const day = Number(match[3]);
	const nextMidnight = new Date(year, month - 1, day + 1).getTime();
	return Math.floor((nextMidnight - 1) / 1000);
}
