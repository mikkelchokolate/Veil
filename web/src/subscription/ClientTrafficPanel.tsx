import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../api/fetcher";

interface ClientTraffic {
	clientId: string;
	uploadBytes: number;
	downloadBytes: number;
	usedBytes: number;
	quotaBytes?: number;
	remainingBytes?: number;
	depleted: boolean;
	collectedAt: number;
}

function fmtBytes(n?: number): string {
	if (n == null) return "—";
	if (n === 0) return "0 B";
	const units = ["B", "KiB", "MiB", "GiB", "TiB"];
	let v = n;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[i]}`;
}

/** B9: per-client traffic usage + quota progress in the client detail Traffic
 * tab. */
export function ClientTrafficPanel({ clientId }: { clientId: string }) {
	const traffic = useQuery<ClientTraffic>({
		queryKey: ["traffic", clientId],
		queryFn: () => apiFetch(`/api/v1/traffic/${clientId}`),
		refetchInterval: 10000,
	});

	if (traffic.isLoading) {
		return (
			<div className="card">
				<p className="muted">Loading…</p>
			</div>
		);
	}
	if (traffic.isError || !traffic.data) {
		return (
			<div className="card">
				<p className="form-error">Traffic unavailable</p>
			</div>
		);
	}

	const t = traffic.data;
	const pct =
		t.quotaBytes && t.quotaBytes > 0
			? Math.min(100, Math.round((t.usedBytes / t.quotaBytes) * 100))
			: null;

	return (
		<div className="card">
			<h2>Traffic usage</h2>
			<p>
				<strong>Upload:</strong> {fmtBytes(t.uploadBytes)}
			</p>
			<p>
				<strong>Download:</strong> {fmtBytes(t.downloadBytes)}
			</p>
			<p>
				<strong>Total used:</strong> {fmtBytes(t.usedBytes)}
			</p>
			{t.quotaBytes != null ? (
				<>
					<p>
						<strong>Quota:</strong> {fmtBytes(t.quotaBytes)}
						{t.remainingBytes != null
							? ` · remaining ${fmtBytes(t.remainingBytes)}`
							: ""}
					</p>
					<div
						style={{
							height: 8,
							borderRadius: 4,
							border: "1px solid var(--border)",
							overflow: "hidden",
							maxWidth: 360,
						}}
						role="progressbar"
						aria-valuenow={pct ?? 0}
						aria-valuemin={0}
						aria-valuemax={100}
					>
						<div
							style={{
								height: "100%",
								width: `${pct ?? 0}%`,
								background: t.depleted
									? "var(--accent-danger)"
									: "var(--accent)",
							}}
						/>
					</div>
					{t.depleted ? (
						<p className="badge badge-danger" style={{ marginTop: 8 }}>
							quota depleted
						</p>
					) : null}
				</>
			) : (
				<p className="muted">No quota configured.</p>
			)}
			<p className="muted" style={{ fontSize: 12, marginTop: 8 }}>
				Collected {new Date(t.collectedAt * 1000).toLocaleTimeString()}
			</p>
		</div>
	);
}
