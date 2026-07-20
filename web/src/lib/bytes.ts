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
