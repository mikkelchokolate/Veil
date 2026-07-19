import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams, Link } from "@tanstack/react-router";
import { apiFetch, ApiError } from "../api/fetcher";
import { useIsAdmin } from "../auth/AuthContext";
import type { ApplyJob } from "../api/generated/models";

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

/** B5: apply job detail page — full job view with retry, plan, and mismatch warning. */
export function ApplyJobDetailPage() {
	const { jobId } = useParams({ strict: false }) as { jobId: string };
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();

	const job = useQuery<ApplyJob>({
		queryKey: ["apply", "jobs", jobId],
		queryFn: () => apiFetch(`/api/apply/jobs/${jobId}`),
		refetchInterval: 5000,
	});

	const retry = useMutation({
		mutationFn: () => apiFetch(`/api/apply/jobs/${jobId}/retry`, { method: "POST" }),
		onSuccess: () => {
			void qc.invalidateQueries({ queryKey: ["apply"] });
		},
	});

	const j = job.data;

	return (
		<>
			<div className="card">
				<p>
					<Link to="/apply" className="muted">← Back to apply jobs</Link>
				</p>
				<h2>Apply job {jobId.slice(0, 8)}</h2>
				{job.isLoading ? (
					<p className="muted">Loading…</p>
				) : job.isError ? (
					<p className="form-error">
						{job.error instanceof ApiError ? job.error.message : "Failed to load job"}
					</p>
				) : j ? (
					<>
						<p>
							<strong>Status:</strong>{" "}
							<span className={`badge ${STATUS_CLS[j.status] ?? ""}`}>{j.status}</span>
						</p>
						<p>
							<strong>Revision:</strong>{" "}
							<span className="mono">{j.baseRevision} → {j.desiredRevision}</span>
						</p>
						<p><strong>Trigger:</strong> <span className="muted">{j.trigger}</span></p>
						{j.actorId ? <p><strong>Actor:</strong> <span className="muted">{j.actorId}</span></p> : null}
						<p><strong>Created:</strong> <span className="muted">{fmtTime(j.createdAt)}</span></p>
						{j.startedAt ? <p><strong>Started:</strong> <span className="muted">{fmtTime(j.startedAt)}</span></p> : null}
						{j.finishedAt ? <p><strong>Finished:</strong> <span className="muted">{fmtTime(j.finishedAt)}</span></p> : null}
						{j.errorMessage ? (
							<p className="form-error">
								{j.errorCode ? `[${j.errorCode}] ` : ""}{j.errorMessage}
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
								{retry.error instanceof ApiError ? retry.error.message : "Retry failed"}
							</p>
						) : null}
						{retry.isSuccess ? (
							<p className="muted">Retry queued — the job will re-render the pinned revision.</p>
						) : null}
					</>
				) : null}
			</div>
		</>
	);
}
