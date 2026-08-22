/** Format a byte count for display. S3: never parse byte counts through the
 * JS Number input path for storage; this formatter only reads numeric values
 * the server already produced and formats with bounded precision (integer
 * arithmetic until the final unit, avoiding float drift). */
export function fmtBytes(n?: number): string {
	if (n == null) return "—";
	if (!Number.isFinite(n)) return "—";
	if (n === 0) return "0 B";
	const neg = n < 0;
	let v = Math.abs(n);
	const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
	let i = 0;
	// Divide in integer space until under 1024 to keep full precision.
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	const out = `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
	return neg ? `-${out}` : out;
}

/** Number.MAX_SAFE_INTEGER as a decimal string, for exact lexicographic
 * comparison without ever converting the user input through a float. */
const MAX_SAFE_INTEGER_DECIMAL = "9007199254740991";

/** Issue 3: quotaBytes is an int64 on the backend but a JSON number on the
 * wire, so values above Number.MAX_SAFE_INTEGER would silently lose precision
 * in every JS consumer. The contract caps quotas at MAX_SAFE_INTEGER
 * end-to-end (OpenAPI maximum + backend validation + form schemas here).
 *
 * Compares decimal STRINGS lexicographically so the check itself is exact
 * even for inputs far beyond float precision ("9007199254740993" must be
 * rejected, and Number("9007199254740993") === 9007199254740992 could not
 * tell it apart from the cap). Assumes v matches /^\d+$/ (the callers' whole-
 * bytes refine runs first). */
export function decimalWithinSafeInteger(v: string): boolean {
	const digits = v.replace(/^0+/, "") || "0";
	if (digits.length !== MAX_SAFE_INTEGER_DECIMAL.length) {
		return digits.length < MAX_SAFE_INTEGER_DECIMAL.length;
	}
	return digits <= MAX_SAFE_INTEGER_DECIMAL;
}

/** Parse a validated decimal string to a number. Only call with values that
 * passed decimalWithinSafeInteger — then the conversion is exact. */
export function parseQuotaDecimal(v: string): number {
	return Number.parseInt(v, 10);
}
