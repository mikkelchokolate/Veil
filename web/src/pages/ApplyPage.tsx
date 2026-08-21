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
import { useI18n } from "../i18n/I18nContext";

function useApplyJobs() {
	return useQuery<{ items: ApplyJob[] }>({
		queryKey: ["apply", "jobs"],
		queryFn: () => apiFetch("/api/apply/jobs"),
		refetchInterval: 2000,
	});
}

function fmtTime(ts?: number): string {
	if (!ts) return "—";
	return new Date(ts * 1000).toLocaleString();
}

function fmtDuration(startedAt?: number, finishedAt?: number): string {
	if (!startedAt || !finishedAt || finishedAt < startedAt) return "—";
	const seconds = finishedAt - startedAt;
	if (seconds < 60) return `${seconds}s`;
	const minutes = Math.floor(seconds / 60);
	const rem = seconds % 60;
	if (minutes < 60) return rem ? `${minutes}m ${rem}s` : `${minutes}m`;
	const hours = Math.floor(minutes / 60);
	return `${hours}h ${minutes % 60}m`;
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
	const { t } = useI18n();
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
				<h2>{t("apply.title")}</h2>
				{state.isLoading ? (
					<p className="muted">{t("common.loading")}</p>
				) : state.isError ? (
					<FormMessage>{t("apply.stateUnavailable")}</FormMessage>
				) : s ? (
					<>
						<p>
							<strong>{t("apply.stateLabel")}:</strong>{" "}
							<Badge variant={STATUS_VARIANT[s.state] ?? "default"}>
								{t(`applyState.${s.state}`)}
							</Badge>
						</p>
						<p>
							<strong>{t("apply.desiredRevisionLabel")}:</strong>{" "}
							<span className="mono">{s.desiredRevision}</span>
						</p>
						<p>
							<strong>{t("apply.appliedRevisionLabel")}:</strong>{" "}
							<span className="mono">{s.appliedRevision}</span>
						</p>
						{drift ? (
							<p>
								<Badge variant="warning">
									{t("apply.driftNotice", {
										applied: s.appliedRevision,
										desired: s.desiredRevision,
									})}
								</Badge>
							</p>
						) : null}
						{s.lastError?.message ? (
							<FormMessage>
								{t("apply.lastError", {
									code: s.lastError.code ? ` (${s.lastError.code})` : "",
									message: s.lastError.message,
								})}
							</FormMessage>
						) : null}
						{isAdmin && drift ? (
							<Button
								variant="primary"
								disabled={reconcile.isPending}
								onClick={() => reconcile.mutate()}
							>
								{reconcile.isPending
									? t("apply.reconciling")
									: t("apply.reconcileNow")}
							</Button>
						) : null}
						{reconcile.isError ? (
							<FormMessage>
								{reconcile.error instanceof ApiError
									? reconcile.error.message
									: t("apply.reconcileFailed")}
							</FormMessage>
						) : null}
					</>
				) : null}
			</div>

			<div className="card">
				<h2>{t("apply.jobsTitle")}</h2>
				{jobs.isLoading ? (
					<p className="muted">{t("common.loading")}</p>
				) : jobs.isError ? (
					<FormMessage>{t("common.error.load")}</FormMessage>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>{t("common.created")}</TableHead>
								<TableHead>{t("apply.revisionHeader")}</TableHead>
								<TableHead>{t("apply.triggerHeader")}</TableHead>
								<TableHead>{t("common.status")}</TableHead>
								<TableHead>{t("apply.durationHeader")}</TableHead>
								<TableHead>{t("apply.errorHeader")}</TableHead>
								{isAdmin ? <TableHead /> : null}
							</TableRow>
						</TableHeader>
						<TableBody>
							{(jobs.data?.items ?? []).length === 0 ? (
								<TableRow>
									<TableCell colSpan={isAdmin ? 7 : 6} className="muted">
										{t("apply.noJobs")}
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
												{(() => {
													const key = `apply.status.${j.status}`;
													const label = t(key);
													return label === key
														? j.status.replaceAll("_", " ")
														: label;
												})()}
											</Badge>
										</TableCell>
										<TableCell className="muted">
											{fmtDuration(j.startedAt, j.finishedAt)}
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
												{j.status === "failed" ||
												j.status === "rollback_failed" ? (
													<Button
														size="sm"
														disabled={retry.isPending}
														onClick={() => retry.mutate(j.id)}
													>
														{t("common.retry")}
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
				{retry.isError ? (
					<FormMessage>
						{retry.error instanceof ApiError
							? retry.error.message
							: t("apply.retryFailed")}
					</FormMessage>
				) : null}
			</div>
		</>
	);
}
