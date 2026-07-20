import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ApiError, apiFetch } from "../api/fetcher";
import type { ApplyJob } from "../api/generated/models";
import { useApplyState } from "../apply/ApplyStatusIndicator";
import { useIsAdmin } from "../auth/AuthContext";

function useApplyJobs() {
	return useQuery<{ items: ApplyJob[] }>({
		queryKey: ["apply", "jobs"],
		queryFn: () => apiFetch("/api/apply/jobs"),
		refetchInterval: 5000,
	});
}

function fmtTime(ts?: number): string {
	if (!ts) return "—";
	return new Date(ts * 1000).toLocaleString();
}

const STATUS_CLS: Record<string, string> = {
	succeeded: "badge-success",
	failed: "badge-danger",
	running: "badge-warning",
	pending: "badge-warning",
};

/** B5: honest synchronous apply semantics — desired vs applied revision,
 * job list with real status, retry, reconcile. No fake queue/202. */
export function ApplyPage() {
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const state = useApplyState();
	const jobs = useApplyJobs();

	const reconcile = useMutation({
		mutationFn: () => apiFetch("/api/apply/reconcile", { method: "POST" }),
		onSuccess: () => {
			void qc.invalidateQueries({ queryKey: ["apply"] });
		},
	});

	const retry = useMutation({
		mutationFn: (jobId: string) =>
			apiFetch(`/api/apply/jobs/${jobId}/retry`, { method: "POST" }),
		onSuccess: () => void qc.invalidateQueries({ queryKey: ["apply"] }),
	});

	const s = state.data;
	const drift = s ? s.desiredRevision !== s.appliedRevision : false;

	return (
		<>
			<div className="card">
				<h2>Apply state</h2>
				{state.isLoading ? (
					<p className="muted">Loading…</p>
				) : state.isError ? (
					<p className="form-error">Apply state unavailable</p>
				) : s ? (
					<>
						<p>
							<strong>State:</strong>{" "}
							<span className={`badge ${STATUS_CLS[s.state] ?? ""}`}>
								{s.state}
							</span>
						</p>
						<p>
							<strong>Desired revision:</strong>{" "}
							<span className="mono">{s.desiredRevision}</span>
						</p>
						<p>
							<strong>Applied (runtime) revision:</strong>{" "}
							<span className="mono">{s.appliedRevision}</span>
						</p>
						{drift ? (
							<p className="badge badge-warning">
								Runtime is behind desired — {s.appliedRevision} →{" "}
								{s.desiredRevision}
							</p>
						) : null}
						{s.lastError?.message ? (
							<p className="form-error">
								Last error{s.lastError.code ? ` (${s.lastError.code})` : ""}:{" "}
								{s.lastError.message}
							</p>
						) : null}
						{isAdmin && drift ? (
							<button
								type="button"
								className="btn btn-primary"
								disabled={reconcile.isPending}
								onClick={() => reconcile.mutate()}
							>
								{reconcile.isPending ? "Reconciling…" : "Reconcile now"}
							</button>
						) : null}
						{reconcile.isError ? (
							<p className="form-error">
								{reconcile.error instanceof ApiError
									? reconcile.error.message
									: "Reconcile failed"}
							</p>
						) : null}
					</>
				) : null}
			</div>

			<div className="card">
				<h2>Apply jobs</h2>
				{jobs.isLoading ? (
					<p className="muted">Loading…</p>
				) : jobs.isError ? (
					<p className="form-error">Failed to load jobs</p>
				) : (
					<div className="table-container">
						<table className="data-table">
							<thead>
								<tr>
									<th>Created</th>
									<th>Revision</th>
									<th>Trigger</th>
									<th>Status</th>
									<th>Error</th>
									{isAdmin ? <th /> : null}
								</tr>
							</thead>
							<tbody>
								{(jobs.data?.items ?? []).length === 0 ? (
									<tr>
										<td colSpan={isAdmin ? 6 : 5} className="muted">
											No apply jobs yet.
										</td>
									</tr>
								) : (
									(jobs.data?.items ?? []).map((j) => (
										<tr key={j.id}>
											<td className="muted">{fmtTime(j.createdAt)}</td>
											<td className="mono">
												<Link
													to="/apply/jobs/$jobId"
													params={{ jobId: j.id }}
													className="mono"
												>
													{j.baseRevision} → {j.desiredRevision}
												</Link>
											</td>
											<td className="muted">{j.trigger}</td>
											<td>
												<span className={`badge ${STATUS_CLS[j.status] ?? ""}`}>
													{j.status}
												</span>
											</td>
											<td className="muted" style={{ maxWidth: 320 }}>
												{j.errorMessage ? (
													<span className="form-error">
														{j.errorCode ? `[${j.errorCode}] ` : ""}
														{j.errorMessage}
													</span>
												) : (
													"—"
												)}
											</td>
											{isAdmin ? (
												<td>
													{j.status === "failed" ? (
														<button
															type="button"
															className="btn"
															disabled={retry.isPending}
															onClick={() => retry.mutate(j.id)}
														>
															Retry
														</button>
													) : null}
												</td>
											) : null}
										</tr>
									))
								)}
							</tbody>
						</table>
					</div>
				)}
			</div>
		</>
	);
}
