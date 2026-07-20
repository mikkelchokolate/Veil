import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import type { ApplyJob } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";

function fmtTime(ts?: number): string {
	if (!ts) return "—";
	return new Date(ts * 1000).toLocaleString();
}

const STATUS_CLS: Record<string, string> = {
	succeeded: "badge-success",
	failed: "badge-danger",
	running: "badge-warning",
	pending: "badge-warning",
	rolled_back: "badge-warning",
};

/** S5: apply job detail — full operation/validation/service/health/rollback
 * breakdown, synchronous retry, plus the rendered plan and the legacy apply
 * history. The generated ApplyJob model omits operations (backend returns
 * them), so they are read defensively here. */
interface OperationResult {
	type: string;
	target?: string;
	success: boolean;
	detail?: string;
}
interface ValidationResult {
	name?: string;
	config?: string;
	valid: boolean;
	error?: string;
}
interface ServiceActionResult {
	command?: string[] | string;
	success: boolean;
	error?: string;
}
interface ServiceHealthResult {
	name: string;
	healthy: boolean;
	error?: string;
}
interface ApplyPlan {
	valid: boolean;
	configs?: string[];
	actions?: string[];
	runtimes?: string[];
	errors?: string[];
	issues?: { message?: string; severity?: string; path?: string }[];
	operations?: {
		type: string;
		source?: string;
		destination?: string;
		unit?: string;
		interruptionRisk?: string;
		rollbackAvailable?: boolean;
	}[];
}
interface HistoryEntry {
	id: string;
	timestamp: string;
	stage: string;
	success: boolean;
	applied: boolean;
	liveApplied: boolean;
	servicesApplied: boolean;
	rolledBack?: boolean;
	plan?: ApplyPlan;
	validations?: ValidationResult[];
	serviceActions?: ServiceActionResult[];
	healthChecks?: ServiceHealthResult[];
	rollbackActions?: ServiceActionResult[];
}

function okBadge(ok: boolean) {
	return (
		<span className={`badge${ok ? " badge-success" : " badge-danger"}`}>
			{ok ? "ok" : "failed"}
		</span>
	);
}

export function ApplyJobDetailPage() {
	const { jobId } = useParams({ strict: false }) as { jobId: string };
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [showPlan, setShowPlan] = useState(false);

	const job = useQuery<ApplyJob>({
		queryKey: ["apply", "jobs", jobId],
		queryFn: () => apiFetch(`/api/apply/jobs/${jobId}`),
		refetchInterval: 5000,
	});

	const plan = useQuery<ApplyPlan>({
		queryKey: ["apply", "plan"],
		queryFn: () =>
			apiFetch("/api/apply/plan", { method: "POST", body: JSON.stringify({}) }),
		enabled: showPlan,
	});

	const history = useQuery<HistoryEntry[] | { items?: HistoryEntry[] }>({
		queryKey: ["apply", "history"],
		queryFn: () => apiFetch("/api/apply/history"),
	});
	const historyItems: HistoryEntry[] = Array.isArray(history.data)
		? history.data
		: (history.data?.items ?? []);

	const retry = useMutation({
		mutationFn: () =>
			apiFetch(`/api/apply/jobs/${jobId}/retry`, { method: "POST" }),
		onSuccess: () => {
			void qc.invalidateQueries({ queryKey: ["apply"] });
		},
	});

	const j = job.data;
	// The backend job record carries the operation breakdown the generated DTO
	// omits; read it defensively.
	const ops: OperationResult[] =
		(j as unknown as { operations?: OperationResult[] })?.operations ?? [];

	return (
		<>
			<div className="card">
				<p>
					<Link to="/apply" className="muted">
						← Back to apply jobs
					</Link>
				</p>
				<h2>Apply job {jobId.slice(0, 8)}</h2>
				{job.isLoading ? (
					<p className="muted">Loading…</p>
				) : job.isError ? (
					<p className="form-error">
						{job.error instanceof ApiError
							? job.error.message
							: "Failed to load job"}
					</p>
				) : j ? (
					<>
						<p>
							<strong>Status:</strong>{" "}
							<span className={`badge ${STATUS_CLS[j.status] ?? ""}`}>
								{j.status}
							</span>
						</p>
						<p>
							<strong>Revision:</strong>{" "}
							<span className="mono">
								{j.baseRevision} → {j.desiredRevision}
							</span>
						</p>
						<p>
							<strong>Trigger:</strong>{" "}
							<span className="muted">{j.trigger}</span>
						</p>
						{j.actorId ? (
							<p>
								<strong>Actor:</strong>{" "}
								<span className="muted">{j.actorId}</span>
							</p>
						) : null}
						<p>
							<strong>Created:</strong>{" "}
							<span className="muted">{fmtTime(j.createdAt)}</span>
						</p>
						{j.startedAt ? (
							<p>
								<strong>Started:</strong>{" "}
								<span className="muted">{fmtTime(j.startedAt)}</span>
							</p>
						) : null}
						{j.finishedAt ? (
							<p>
								<strong>Finished:</strong>{" "}
								<span className="muted">{fmtTime(j.finishedAt)}</span>
							</p>
						) : null}
						{j.errorMessage ? (
							<p className="form-error">
								{j.errorCode ? `[${j.errorCode}] ` : ""}
								{j.errorMessage}
							</p>
						) : null}
						{isAdmin && j.status === "failed" ? (
							<button
								type="button"
								className="btn btn-primary"
								disabled={retry.isPending}
								onClick={() => retry.mutate()}
							>
								{retry.isPending ? "Retrying…" : "Retry this revision"}
							</button>
						) : null}
						{retry.isError ? (
							<p className="form-error">
								{retry.error instanceof ApiError
									? retry.error.message
									: "Retry failed"}
							</p>
						) : null}
						{retry.isSuccess ? (
							<p className="muted">
								Retry queued — the job will re-render the pinned revision.
							</p>
						) : null}
					</>
				) : null}
			</div>

			{/* S5: operation breakdown for this job */}
			{ops.length > 0 ? (
				<div className="card">
					<h2 style={{ fontSize: 15 }}>Operations</h2>
					<div className="table-container">
						<table className="data-table">
							<thead>
								<tr>
									<th>Type</th>
									<th>Target</th>
									<th>Result</th>
									<th>Detail</th>
								</tr>
							</thead>
							<tbody>
								{ops.map((op, i) => (
									// biome-ignore lint/suspicious/noArrayIndexKey: stable prefix + index dedup for API rows without ids
									<tr key={`${op.type}-${op.target ?? ""}-${i}`}>
										<td className="muted">{op.type}</td>
										<td className="mono" style={{ fontSize: 12 }}>
											{op.target ?? "—"}
										</td>
										<td>{okBadge(op.success)}</td>
										<td className="muted" style={{ maxWidth: 360 }}>
											{op.detail ?? ""}
										</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				</div>
			) : null}

			{/* S5: rendered plan (on demand) */}
			<div className="card">
				<div style={{ display: "flex", alignItems: "center", gap: 12 }}>
					<h2 style={{ margin: 0, fontSize: 15, flex: 1 }}>Rendered plan</h2>
					<button
						type="button"
						className="btn"
						onClick={() => setShowPlan((v) => !v)}
					>
						{showPlan ? "Hide" : "Show plan"}
					</button>
				</div>
				{showPlan ? (
					plan.isLoading ? (
						<p className="muted">Loading plan…</p>
					) : plan.isError ? (
						<p className="form-error">Failed to load plan</p>
					) : plan.data ? (
						<>
							<p>
								{okBadge(plan.data.valid)}{" "}
								<span className="muted">
									{plan.data.configs?.length ?? 0} config(s) ·{" "}
									{plan.data.actions?.length ?? 0} action(s)
								</span>
							</p>
							{plan.data.errors && plan.data.errors.length > 0 ? (
								<ul className="form-error">
									{plan.data.errors.map((e) => (
										<li key={e}>{e}</li>
									))}
								</ul>
							) : null}
							{plan.data.issues && plan.data.issues.length > 0 ? (
								<ul>
									{plan.data.issues.map((iss, i) => (
										// biome-ignore lint/suspicious/noArrayIndexKey: stable prefix + index dedup for API rows without ids
										<li key={`${iss.path ?? ""}-${i}`} className="muted">
											[{iss.severity ?? "info"}] {iss.path ?? ""}{" "}
											{iss.message ?? ""}
										</li>
									))}
								</ul>
							) : null}
							{plan.data.operations && plan.data.operations.length > 0 ? (
								<div className="table-container" style={{ marginTop: 8 }}>
									<table className="data-table">
										<thead>
											<tr>
												<th>Type</th>
												<th>Source</th>
												<th>Destination</th>
												<th>Unit</th>
												<th>Risk</th>
												<th>Rollback</th>
											</tr>
										</thead>
										<tbody>
											{plan.data.operations.map((op, i) => (
												// biome-ignore lint/suspicious/noArrayIndexKey: stable prefix + index dedup for API rows without ids
												<tr key={`${op.type}-${op.destination ?? ""}-${i}`}>
													<td className="muted">{op.type}</td>
													<td className="mono" style={{ fontSize: 12 }}>
														{op.source ?? "—"}
													</td>
													<td className="mono" style={{ fontSize: 12 }}>
														{op.destination ?? "—"}
													</td>
													<td className="muted">{op.unit ?? "—"}</td>
													<td className="muted">
														{op.interruptionRisk ?? "—"}
													</td>
													<td className="muted">
														{op.rollbackAvailable ? "yes" : "no"}
													</td>
												</tr>
											))}
										</tbody>
									</table>
								</div>
							) : null}
						</>
					) : null
				) : null}
			</div>

			{/* S5: legacy apply history with full validation/service/health/rollback */}
			<div className="card">
				<h2 style={{ fontSize: 15 }}>Apply history (legacy)</h2>
				{history.isLoading ? (
					<p className="muted">Loading…</p>
				) : historyItems.length === 0 ? (
					<p className="muted">No history entries.</p>
				) : (
					historyItems.map((h) => (
						<details key={h.id} style={{ marginBottom: 8 }}>
							<summary style={{ cursor: "pointer" }}>
								{okBadge(h.success)}{" "}
								<span className="muted">
									{new Date(h.timestamp).toLocaleString()} · {h.stage}
									{h.rolledBack ? " · rolled back" : ""}
								</span>
							</summary>
							<div style={{ paddingLeft: 16, marginTop: 8 }}>
								<p className="muted" style={{ fontSize: 13 }}>
									applied {String(h.applied)} · live {String(h.liveApplied)} ·
									services {String(h.servicesApplied)}
								</p>
								{h.validations && h.validations.length > 0 ? (
									<>
										<strong style={{ fontSize: 13 }}>Validations</strong>
										<ul>
											{h.validations.map((v, i) => (
												// biome-ignore lint/suspicious/noArrayIndexKey: stable prefix + index dedup for API rows without ids
												<li key={`${v.name ?? v.config ?? ""}-${i}`}>
													{okBadge(v.valid)}{" "}
													<span className="mono" style={{ fontSize: 12 }}>
														{v.name ?? v.config}
													</span>{" "}
													{v.error ? (
														<span className="form-error">{v.error}</span>
													) : null}
												</li>
											))}
										</ul>
									</>
								) : null}
								{h.serviceActions && h.serviceActions.length > 0 ? (
									<>
										<strong style={{ fontSize: 13 }}>Service actions</strong>
										<ul>
											{h.serviceActions.map((a, i) => (
												<li
													// biome-ignore lint/suspicious/noArrayIndexKey: stable prefix + index dedup for API rows without ids
													key={`${Array.isArray(a.command) ? a.command.join(" ") : a.command}-${i}`}
												>
													{okBadge(a.success)}{" "}
													<span className="mono" style={{ fontSize: 12 }}>
														{Array.isArray(a.command)
															? a.command.join(" ")
															: a.command}
													</span>{" "}
													{a.error ? (
														<span className="form-error">{a.error}</span>
													) : null}
												</li>
											))}
										</ul>
									</>
								) : null}
								{h.healthChecks && h.healthChecks.length > 0 ? (
									<>
										<strong style={{ fontSize: 13 }}>Health checks</strong>
										<ul>
											{h.healthChecks.map((hc) => (
												<li key={hc.name}>
													{okBadge(hc.healthy)}{" "}
													<span className="mono" style={{ fontSize: 12 }}>
														{hc.name}
													</span>{" "}
													{hc.error ? (
														<span className="form-error">{hc.error}</span>
													) : null}
												</li>
											))}
										</ul>
									</>
								) : null}
								{h.rollbackActions && h.rollbackActions.length > 0 ? (
									<>
										<strong style={{ fontSize: 13 }}>Rollback actions</strong>
										<ul>
											{h.rollbackActions.map((a, i) => (
												<li
													// biome-ignore lint/suspicious/noArrayIndexKey: stable prefix + index dedup for API rows without ids
													key={`rb-${Array.isArray(a.command) ? a.command.join(" ") : a.command}-${i}`}
												>
													{okBadge(a.success)}{" "}
													<span className="mono" style={{ fontSize: 12 }}>
														{Array.isArray(a.command)
															? a.command.join(" ")
															: a.command}
													</span>
												</li>
											))}
										</ul>
									</>
								) : null}
							</div>
						</details>
					))
				)}
			</div>
		</>
	);
}
