import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ApiError, apiFetch } from "../api/fetcher";
import type { ApplyJob } from "../api/generated/models";
import { useApplyState } from "../apply/ApplyStatusIndicator";
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

const STATUS_VARIANT: Record<
	string,
	"success" | "danger" | "warning" | "default"
> = {
	succeeded: "success",
	failed: "danger",
	running: "warning",
	pending: "warning",
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
					<FormMessage>Apply state unavailable</FormMessage>
				) : s ? (
					<>
						<p>
							<strong>State:</strong>{" "}
							<Badge variant={STATUS_VARIANT[s.state] ?? "default"}>
								{s.state}
							</Badge>
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
							<p>
								<Badge variant="warning">
									Runtime is behind desired — {s.appliedRevision} →{" "}
									{s.desiredRevision}
								</Badge>
							</p>
						) : null}
						{s.lastError?.message ? (
							<FormMessage>
								Last error{s.lastError.code ? ` (${s.lastError.code})` : ""}:{" "}
								{s.lastError.message}
							</FormMessage>
						) : null}
						{isAdmin && drift ? (
							<Button
								variant="primary"
								disabled={reconcile.isPending}
								onClick={() => reconcile.mutate()}
							>
								{reconcile.isPending ? "Reconciling…" : "Reconcile now"}
							</Button>
						) : null}
						{reconcile.isError ? (
							<FormMessage>
								{reconcile.error instanceof ApiError
									? reconcile.error.message
									: "Reconcile failed"}
							</FormMessage>
						) : null}
					</>
				) : null}
			</div>

			<div className="card">
				<h2>Apply jobs</h2>
				{jobs.isLoading ? (
					<p className="muted">Loading…</p>
				) : jobs.isError ? (
					<FormMessage>Failed to load jobs</FormMessage>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Created</TableHead>
								<TableHead>Revision</TableHead>
								<TableHead>Trigger</TableHead>
								<TableHead>Status</TableHead>
								<TableHead>Error</TableHead>
								{isAdmin ? <TableHead /> : null}
							</TableRow>
						</TableHeader>
						<TableBody>
							{(jobs.data?.items ?? []).length === 0 ? (
								<TableRow>
									<TableCell colSpan={isAdmin ? 6 : 5} className="muted">
										No apply jobs yet.
									</TableCell>
								</TableRow>
							) : (
								(jobs.data?.items ?? []).map((j) => (
									<TableRow key={j.id}>
										<TableCell className="muted">
											{fmtTime(j.createdAt)}
										</TableCell>
										<TableCell className="mono">
											<Link
												to="/apply/$jobId"
												params={{ jobId: j.id }}
												className="mono"
											>
												{j.baseRevision} → {j.desiredRevision}
											</Link>
										</TableCell>
										<TableCell className="muted">{j.trigger}</TableCell>
										<TableCell>
											<Badge variant={STATUS_VARIANT[j.status] ?? "default"}>
												{j.status}
											</Badge>
										</TableCell>
										<TableCell className="muted" style={{ maxWidth: 320 }}>
											{j.errorMessage ? (
												<span className="text-[var(--danger)]">
													{j.errorCode ? `[${j.errorCode}] ` : ""}
													{j.errorMessage}
												</span>
											) : (
												"—"
											)}
										</TableCell>
										{isAdmin ? (
											<TableCell>
												{j.status === "failed" ? (
													<Button
														size="sm"
														disabled={retry.isPending}
														onClick={() => retry.mutate(j.id)}
													>
														Retry
													</Button>
												) : null}
											</TableCell>
										) : null}
									</TableRow>
								))
							)}
						</TableBody>
					</Table>
				)}
			</div>
		</>
	);
}
