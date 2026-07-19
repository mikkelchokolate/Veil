import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../api/fetcher";

interface TrafficSummary {
	state: string;
	providerCount?: number;
	uploadBytes?: number;
	downloadBytes?: number;
	usedBytes?: number;
}

interface TopEntry {
	clientId: string;
	name: string;
	uploadBytes: number;
	downloadBytes: number;
	usedBytes: number;
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

/** B9: traffic dashboard over honest telemetry states. When no runtime feeds
 * counters the panel says so explicitly instead of rendering a fake graph. */
export function TrafficPage() {
	const summary = useQuery<TrafficSummary>({
		queryKey: ["traffic", "summary"],
		queryFn: () => apiFetch("/api/v1/traffic/summary"),
		refetchInterval: 10000,
	});
	const top = useQuery<{ items: TopEntry[] }>({
		queryKey: ["traffic", "top"],
		queryFn: () => apiFetch("/api/v1/traffic/top"),
		refetchInterval: 10000,
		enabled: summary.data?.state === "collecting",
	});

	const state = summary.data?.state;

	return (
		<>
			<div className="card">
				<h2>Traffic telemetry</h2>
				{summary.isLoading ? (
					<p className="muted">Loading…</p>
				) : summary.isError ? (
					<p className="form-error">Traffic summary unavailable</p>
				) : summary.data ? (
					<>
						<p>
							<strong>Telemetry state:</strong>{" "}
							<span className={`badge${state === "collecting" ? " badge-success" : " badge-warning"}`}>
								{state}
							</span>
						</p>
						{state !== "collecting" ? (
							<p className="muted">
								No traffic source is feeding counters yet. Usage figures below are not real telemetry —
								configure a runtime traffic provider to begin collecting.
							</p>
						) : (
							<>
								<p><strong>Total upload:</strong> {fmtBytes(summary.data.uploadBytes)}</p>
								<p><strong>Total download:</strong> {fmtBytes(summary.data.downloadBytes)}</p>
								<p><strong>Total used:</strong> {fmtBytes(summary.data.usedBytes)}</p>
							</>
						)}
					</>
				) : null}
			</div>

			{state === "collecting" ? (
				<div className="card">
					<h2>Top clients by usage</h2>
					{top.isLoading ? (
						<p className="muted">Loading…</p>
					) : (
						<div className="table-container">
							<table className="data-table">
								<thead>
									<tr>
										<th>Client</th>
										<th>Upload</th>
										<th>Download</th>
										<th>Total</th>
									</tr>
								</thead>
								<tbody>
									{(top.data?.items ?? []).length === 0 ? (
										<tr>
											<td colSpan={4} className="muted">
												No usage recorded yet.
											</td>
										</tr>
									) : (
										(top.data?.items ?? []).map((t) => (
											<tr key={t.clientId}>
												<td>{t.name}</td>
												<td className="muted">{fmtBytes(t.uploadBytes)}</td>
												<td className="muted">{fmtBytes(t.downloadBytes)}</td>
												<td className="muted">{fmtBytes(t.usedBytes)}</td>
											</tr>
										))
									)}
								</tbody>
							</table>
						</div>
					)}
				</div>
			) : null}
		</>
	);
}
