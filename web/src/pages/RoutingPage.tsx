import { useQuery } from "@tanstack/react-query";
import { apiFetch, ApiError } from "../api/fetcher";

interface RoutingRule {
	name: string;
	match: string;
	outbound: string;
	enabled: boolean;
}

/** Routing rules: read-only list. Editing staged routing is a higher-risk
 * mutation surface; list + status shown here. */
export function RoutingPage() {
	const rules = useQuery<RoutingRule[]>({
		queryKey: ["routing", "rules"],
		queryFn: () => apiFetch("/api/routing/rules"),
	});

	return (
		<div className="card">
			<h2>Routing rules</h2>
			{rules.isLoading ? (
				<p className="muted">Loading…</p>
			) : rules.isError ? (
				<p className="form-error">
					{rules.error instanceof ApiError ? rules.error.message : "Failed to load routing rules"}
				</p>
			) : (
				<div className="table-container">
					<table className="data-table">
						<thead>
							<tr>
								<th>Name</th>
								<th>Match</th>
								<th>Outbound</th>
								<th>Status</th>
							</tr>
						</thead>
						<tbody>
							{(rules.data ?? []).length === 0 ? (
								<tr>
									<td colSpan={4} className="muted">
										No routing rules configured.
									</td>
								</tr>
							) : (
								(rules.data ?? []).map((r) => (
									<tr key={r.name}>
										<td>{r.name}</td>
										<td className="mono muted">{r.match}</td>
										<td className="muted">{r.outbound}</td>
										<td>
											<span className={`badge${r.enabled ? " badge-success" : ""}`}>
												{r.enabled ? "enabled" : "disabled"}
											</span>
										</td>
									</tr>
								))
							)}
						</tbody>
					</table>
				</div>
			)}
		</div>
	);
}
