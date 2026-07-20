import { useQuery } from "@tanstack/react-query";
import { ApiError, apiFetch } from "../api/fetcher";
import type { Inbound } from "../api/generated/models";

/** B10: inbounds list. Legacy embedded profiles are no longer edited here —
 * normalized clients are managed under Clients; attached clients shown. */
export function InboundsPage() {
	const inbounds = useQuery<Inbound[]>({
		queryKey: ["inbounds", "all"],
		queryFn: () => apiFetch("/api/inbounds"),
	});

	return (
		<div className="card">
			<h2>Inbounds</h2>
			{inbounds.isLoading ? (
				<p className="muted">Loading…</p>
			) : inbounds.isError ? (
				<p className="form-error">
					{inbounds.error instanceof ApiError
						? inbounds.error.message
						: "Failed to load inbounds"}
				</p>
			) : (
				<div className="table-container">
					<table className="data-table">
						<thead>
							<tr>
								<th>Name</th>
								<th>Protocol</th>
								<th>Transport</th>
								<th>Port</th>
								<th>Status</th>
							</tr>
						</thead>
						<tbody>
							{(inbounds.data ?? []).length === 0 ? (
								<tr>
									<td colSpan={5} className="muted">
										No inbounds configured.
									</td>
								</tr>
							) : (
								(inbounds.data ?? []).map((ib) => (
									<tr key={ib.name}>
										<td>{ib.name}</td>
										<td className="muted">{ib.protocol}</td>
										<td className="muted">{ib.transport ?? "—"}</td>
										<td className="muted">{ib.port ?? "—"}</td>
										<td>
											<span
												className={`badge${ib.enabled ? " badge-success" : ""}`}
											>
												{ib.enabled ? "enabled" : "disabled"}
											</span>
										</td>
									</tr>
								))
							)}
						</tbody>
					</table>
				</div>
			)}
			<p className="muted" style={{ marginTop: 12, fontSize: 13 }}>
				Client credentials are managed per-client under Clients → Access, not by
				editing inbound profiles (B10).
			</p>
		</div>
	);
}
