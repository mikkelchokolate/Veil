import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import type { ApplyJob } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { FormMessage } from "../components/ui/form";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "../components/ui/table";

function fmtTime(ts?: number): string {
	if (!ts) return "—";
	return new Date(ts * 1000).toLocaleString();
}

const STATUS_VARIANT: Record<
	string,
	"success" | "danger" | "warning" | "default"
> = {
	succeeded: "success",
	failed: "danger",
	running: "warning",
	pending: "warning",
	rolled_back: "warning",
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
interface ApplyPlanOperation {
	type: string;
	source?: string;
	destination?: string;
	unit?: string;
	interruptionRisk?: string;
	rollbackAvailable?: boolean;
}
interface ApplyPlan {
	valid: boolean;
	configs?: string[];
	actions?: string[];
	runtimes?: string[];
	errors?: string[];
	issues?: { message?: string; severity?: string; path?: string }[];
	operations?: ApplyPlanOperation[];
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
		<Badge variant={ok ? "success" : "danger"}>{ok ? "ok" : "failed"}</Badge>
	);
}

/** Plan rows arrive without ids; stable type+destination prefix plus the row
 * index is the dedup the rest of this page already uses. */
const planOpKey = (op: ApplyPlanOperation, i: number) =>
	`${op.type}-${op.destination ?? ""}-${i}`;

function PlanOpRow({ op }: { op: ApplyPlanOperation }) {
	return (
		<TableRow>
			<TableCell className="muted">{op.type}</TableCell>
			<TableCell className="mono" style={{ fontSize: 12 }}>
				{op.source ?? "—"}
			</TableCell>
			<TableCell className="mono" style={{ fontSize: 12 }}>
				{op.destination ?? "—"}
			</TableCell>
			<TableCell className="muted">{op.unit ?? "—"}</TableCell>
			<TableCell className="muted">{op.interruptionRisk ?? "—"}</TableCell>
			<TableCell className="muted">
				{op.rollbackAvailable ? "yes" : "no"}
			</TableCell>
		</TableRow>
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
					<FormMessage>
						{job.error instanceof ApiError
							? job.error.message
							: "Failed to load job"}
					</FormMessage>
				) : j ? (
					<>
						<p>
							<strong>Status:</strong>{" "}
							<Badge variant={STATUS_VARIANT[j.status] ?? "default"}>
								{j.status}
							</Badge>
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
							<FormMessage>
								{j.errorCode ? `[${j.errorCode}] ` : ""}
								{j.errorMessage}
							</FormMessage>
						) : null}
						{isAdmin && j.status === "failed" ? (
							<Button
								variant="primary"
								disabled={retry.isPending}
								onClick={() => retry.mutate()}
							>
								{retry.isPending ? "Retrying…" : "Retry this revision"}
							</Button>
						) : null}
						{retry.isError ? (
							<FormMessage>
								{retry.error instanceof ApiError
									? retry.error.message
									: "Retry failed"}
							</FormMessage>
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
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Type</TableHead>
								<TableHead>Target</TableHead>
								<TableHead>Result</TableHead>
								<TableHead>Detail</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{ops.map((op, i) => (
								// biome-ignore lint/suspicious/noArrayIndexKey: stable prefix + index dedup for API rows without ids
								<TableRow key={`${op.type}-${op.target ?? ""}-${i}`}>
									<TableCell className="muted">{op.type}</TableCell>
									<TableCell className="mono" style={{ fontSize: 12 }}>
										{op.target ?? "—"}
									</TableCell>
									<TableCell>{okBadge(op.success)}</TableCell>
									<TableCell className="muted" style={{ maxWidth: 360 }}>
										{op.detail ?? ""}
									</TableCell>
								</TableRow>
							))}
						</TableBody>
					</Table>
				</div>
			) : null}

			{/* S5: rendered plan (on demand) */}
			<div className="card">
				<div style={{ display: "flex", alignItems: "center", gap: 12 }}>
					<h2 style={{ margin: 0, fontSize: 15, flex: 1 }}>Rendered plan</h2>
					<Button onClick={() => setShowPlan((v) => !v)}>
						{showPlan ? "Hide" : "Show plan"}
					</Button>
				</div>
				{showPlan ? (
					plan.isLoading ? (
						<p className="muted">Loading plan…</p>
					) : plan.isError ? (
						<FormMessage>Failed to load plan</FormMessage>
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
								<ul className="text-[var(--danger)]">
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
								<div style={{ marginTop: 8 }}>
									<Table>
										<TableHeader>
											<TableRow>
												<TableHead>Type</TableHead>
												<TableHead>Source</TableHead>
												<TableHead>Destination</TableHead>
												<TableHead>Unit</TableHead>
												<TableHead>Risk</TableHead>
												<TableHead>Rollback</TableHead>
											</TableRow>
										</TableHeader>
										<TableBody>
											{plan.data.operations.map((op, i) => (
												<PlanOpRow key={planOpKey(op, i)} op={op} />
											))}
										</TableBody>
									</Table>
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
														<span className="text-[var(--danger)]">
															{v.error}
														</span>
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
														<span className="text-[var(--danger)]">
															{a.error}
														</span>
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
														<span className="text-[var(--danger)]">
															{hc.error}
														</span>
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
