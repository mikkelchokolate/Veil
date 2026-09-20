import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
	ApiError,
	apiFetch,
	apiUrl,
	mutationErrorMessage,
} from "../api/fetcher";
import type { BackupArchive } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "../components/ui/alert-dialog";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { FormItem, FormMessage } from "../components/ui/form";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "../components/ui/table";
import { useI18n } from "../i18n/I18nContext";
import { fmtBytes } from "../lib/bytes";

interface RestoreJob {
	id: string;
	archive: string;
	status: string;
	// Restore pipeline detail emitted by the API (#586): a degraded job with
	// restored=true committed state and must not look like a hard failure.
	outcome?: string;
	phase?: string;
	restored?: boolean;
	httpStatus?: number;
	createdAt: string;
	finishedAt?: string;
	error?: string;
}

interface VerifyResult {
	ok?: boolean;
	valid?: boolean;
	error?: string;
	message?: string;
}

/** S4: full backup lifecycle — list/create, download, verify, restore (with
 * confirm), prune, and restore job tracking. */
export function BackupsPage() {
	const { t } = useI18n();
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [error, setError] = useState<string | null>(null);
	const [notice, setNotice] = useState<string | null>(null);
	const [verifyResult, setVerifyResult] = useState<
		Record<string, VerifyResult>
	>({});
	const [confirmRestore, setConfirmRestore] = useState<string | null>(null);
	const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
	const [confirmPrune, setConfirmPrune] = useState(false);
	const [pruneDaily, setPruneDaily] = useState("7");
	const [pruneWeekly, setPruneWeekly] = useState("4");
	const [pruneMonthly, setPruneMonthly] = useState("12");
	const [activeJob, setActiveJob] = useState<RestoreJob | null>(null);

	const backups = useQuery<{ items?: BackupArchive[] } | BackupArchive[]>({
		queryKey: ["backups"],
		queryFn: () => apiFetch("/api/backups"),
	});
	const items: BackupArchive[] = Array.isArray(backups.data)
		? backups.data
		: (backups.data?.items ?? []);

	// Poll a queued/running restore job until it finishes.
	const jobQuery = useQuery<RestoreJob>({
		queryKey: ["backup-restore-job", activeJob?.id],
		queryFn: async () => {
			try {
				return await apiFetch<RestoreJob>(
					`/api/backup-restore-jobs/${activeJob?.id}`,
				);
			} catch (err) {
				if (
					err instanceof ApiError &&
					err.body &&
					typeof err.body === "object" &&
					"status" in (err.body as object)
				) {
					return err.body as RestoreJob;
				}
				throw err;
			}
		},
		enabled: !!activeJob,
		refetchInterval: (query) => {
			const status = query.state.data?.status ?? activeJob?.status;
			return status === "queued" || status === "running" ? 1500 : false;
		},
	});
	const job = jobQuery.data ?? activeJob;
	const jobStatusKey = job ? `backups.status.${job.status}` : "";
	// A malformed poll payload (no status) must not crash the render — fall
	// back to an empty label instead of calling replaceAll on undefined.
	const jobStatusLabel =
		job && t(jobStatusKey) === jobStatusKey
			? (job.status ?? "").replaceAll("_", " ")
			: job
				? t(jobStatusKey)
				: "";
	const jobSettled =
		job?.status === "succeeded" ||
		job?.status === "failed" ||
		job?.status === "degraded" ||
		job?.status === "pending";

	const create = useMutation({
		mutationFn: () =>
			apiFetch("/api/backups", { method: "POST", body: JSON.stringify({}) }),
		onSuccess: () => {
			setError(null);
			setNotice(t("backups.notice.created"));
			void qc.invalidateQueries({ queryKey: ["backups"] });
		},
		onError: (e) =>
			setError(mutationErrorMessage(e, t("backups.error.create"))),
	});

	function parseRetention(value: string, fallback: number): number {
		const n = Number.parseInt(value, 10);
		return Number.isFinite(n) && n >= 0 ? n : fallback;
	}

	const pruneRetention = {
		daily: parseRetention(pruneDaily, 7),
		weekly: parseRetention(pruneWeekly, 4),
		monthly: parseRetention(pruneMonthly, 12),
	};

	const prune = useMutation({
		mutationFn: () =>
			apiFetch("/api/backups/prune", {
				method: "POST",
				body: JSON.stringify(pruneRetention),
			}),
		onSuccess: () => {
			setConfirmPrune(false);
			setError(null);
			setNotice(t("backups.notice.pruned"));
			void qc.invalidateQueries({ queryKey: ["backups"] });
		},
		onError: (e) => setError(mutationErrorMessage(e, t("backups.error.prune"))),
	});

	const verify = useMutation({
		mutationFn: async (name: string) => {
			const res = await apiFetch(
				`/api/backups/${encodeURIComponent(name)}/verify`,
				{
					method: "POST",
					body: JSON.stringify({}),
				},
			);
			// A non-2xx response is rejected by apiFetch. A successfully decoded
			// verification report therefore means the archive is valid even though
			// the report contract carries metadata rather than an `ok` flag.
			return { name, res: { ...(res as VerifyResult), valid: true } };
		},
		onSuccess: ({ name, res }) => {
			setError(null);
			setVerifyResult((prev) => ({ ...prev, [name]: res }));
		},
		onError: (e) =>
			setError(mutationErrorMessage(e, t("backups.error.verify"))),
	});

	const remove = useMutation({
		mutationFn: (name: string) =>
			apiFetch(`/api/backups/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			setConfirmDelete(null);
			setError(null);
			setNotice(t("backups.notice.deleted"));
			void qc.invalidateQueries({ queryKey: ["backups"] });
		},
		onError: (e) =>
			setError(mutationErrorMessage(e, t("backups.error.delete"))),
	});

	const restore = useMutation({
		mutationFn: async (name: string) => {
			const res = await apiFetch(
				`/api/backups/${encodeURIComponent(name)}/restore`,
				{
					method: "POST",
					body: JSON.stringify({ confirm: true }),
				},
			);
			return res as RestoreJob;
		},
		onSuccess: (j) => {
			setConfirmRestore(null);
			setError(null);
			setNotice(t("backups.notice.queued", { archive: j.archive }));
			setActiveJob(j);
		},
		onError: (e) =>
			setError(mutationErrorMessage(e, t("backups.error.restore"))),
	});

	async function download(name: string) {
		try {
			const res = await fetch(
				apiUrl(`/api/backups/${encodeURIComponent(name)}/download`),
				{
					credentials: "same-origin",
				},
			);
			if (!res.ok) throw new Error(`download failed: ${res.status}`);
			const blob = await res.blob();
			const url = URL.createObjectURL(blob);
			const a = document.createElement("a");
			a.href = url;
			a.download = name;
			a.click();
			URL.revokeObjectURL(url);
			setError(null);
		} catch (e) {
			setError(e instanceof Error ? e.message : t("backups.error.download"));
		}
	}

	if (!isAdmin) {
		return (
			<div className="card">
				<h2>{t("backups.title")}</h2>
				<p className="muted">{t("backups.adminRequired")}</p>
			</div>
		);
	}

	return (
		<>
			<div className="card">
				<div className="h-scroll" style={{ gap: 8 }}>
					<h2 style={{ margin: 0, flex: 1 }}>{t("backups.title")}</h2>
					{isAdmin ? (
						<>
							<Button
								disabled={prune.isPending}
								onClick={() => setConfirmPrune(true)}
							>
								{prune.isPending ? t("backups.pruning") : t("backups.prune")}
							</Button>
							<Button
								variant="primary"
								disabled={create.isPending}
								onClick={() => create.mutate()}
							>
								{create.isPending ? t("backups.creating") : t("backups.create")}
							</Button>
						</>
					) : null}
				</div>
				{notice ? <p className="muted">{notice}</p> : null}
				{error ? <FormMessage>{error}</FormMessage> : null}
				<p className="muted">{t("backups.hint")}</p>
			</div>

			{job ? (
				<div className="card">
					<h2 style={{ fontSize: 15 }}>{t("backups.restoreJobTitle")}</h2>
					<p>
						<Badge
							variant={
								job.status === "succeeded"
									? "success"
									: job.status === "failed" ||
										  (job.status === "degraded" && !job.restored)
										? "danger"
										: "warning"
							}
						>
							{jobStatusLabel}
						</Badge>{" "}
						<span className="mono">{job.archive}</span>
					</p>
					{/* A degraded restore that still committed state must surface
					    as a warning success, not a hard fail (#586). */}
					{job.outcome || job.phase ? (
						<p className="muted">
							{job.outcome
								? t("backups.restoreJobOutcome", {
										outcome: job.outcome.replaceAll("_", " "),
									})
								: null}
							{job.phase
								? ` ${t("backups.restoreJobPhase", {
										phase: job.phase.replaceAll("_", " "),
									})}`
								: null}
						</p>
					) : null}
					{job.restored && job.status === "degraded" ? (
						<p className="muted">{t("backups.restoreJobDegraded")}</p>
					) : null}
					{jobQuery.isError ? (
						<FormMessage>
							{jobQuery.error instanceof ApiError
								? jobQuery.error.message
								: t("backups.error.restoreJob")}
						</FormMessage>
					) : null}
					{job.error ? <FormMessage>{job.error}</FormMessage> : null}
					{jobSettled ? (
						<Button
							onClick={() => {
								setActiveJob(null);
								void qc.invalidateQueries({ queryKey: ["backups"] });
							}}
						>
							{t("backups.dismiss")}
						</Button>
					) : null}
				</div>
			) : null}

			<div className="card">
				{backups.isLoading ? (
					<p className="muted">{t("common.loading")}</p>
				) : backups.isError ? (
					<FormMessage>
						{backups.error instanceof ApiError
							? backups.error.message
							: t("backups.error.load")}
					</FormMessage>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>{t("common.name")}</TableHead>
								<TableHead>{t("backups.size")}</TableHead>
								<TableHead>{t("common.created")}</TableHead>
								<TableHead>{t("backups.encrypted")}</TableHead>
								<TableHead>{t("backups.verify")}</TableHead>
								{isAdmin ? <TableHead>{t("common.actions")}</TableHead> : null}
							</TableRow>
						</TableHeader>
						<TableBody>
							{items.length === 0 ? (
								<TableRow>
									<TableCell colSpan={6} className="muted">
										{t("backups.empty")}
									</TableCell>
								</TableRow>
							) : (
								items.map((b) => {
									const v = verifyResult[b.name];
									return (
										<TableRow key={b.name}>
											<TableCell className="mono">{b.name}</TableCell>
											<TableCell className="muted">
												{fmtBytes(b.size)}
											</TableCell>
											<TableCell className="muted">
												{new Date(b.createdAt).toLocaleString()}
											</TableCell>
											<TableCell>
												<Badge variant={b.encrypted ? "success" : "default"}>
													{b.encrypted ? t("common.yes") : t("common.no")}
												</Badge>
											</TableCell>
											<TableCell>
												{v ? (
													<Badge
														variant={v.ok || v.valid ? "success" : "danger"}
													>
														{v.ok || v.valid
															? t("backups.verifyOk")
															: (v.error ??
																v.message ??
																t("backups.verifyInvalid"))}
													</Badge>
												) : (
													<span className="muted">—</span>
												)}
											</TableCell>
											{isAdmin ? (
												<TableCell>
													<div
														style={{
															display: "flex",
															gap: 6,
															flexWrap: "wrap",
														}}
													>
														<Button
															size="sm"
															onClick={() => void download(b.name)}
														>
															{t("backups.download")}
														</Button>
														<Button
															size="sm"
															disabled={verify.isPending}
															onClick={() => verify.mutate(b.name)}
														>
															{t("backups.verify")}
														</Button>
														<Button
															size="sm"
															variant="danger"
															onClick={() => setConfirmRestore(b.name)}
														>
															{t("backups.restore")}
														</Button>
														<Button
															size="sm"
															variant="danger"
															onClick={() => setConfirmDelete(b.name)}
														>
															{t("common.delete")}
														</Button>
													</div>
												</TableCell>
											) : null}
										</TableRow>
									);
								})
							)}
						</TableBody>
					</Table>
				)}
			</div>

			<AlertDialog
				open={confirmPrune}
				onOpenChange={(open) => {
					if (!open) setConfirmPrune(false);
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>
							{t("backups.pruneConfirmTitle")}
						</AlertDialogTitle>
						<AlertDialogDescription>
							{t("backups.pruneConfirmDescription", {
								daily: pruneRetention.daily,
								weekly: pruneRetention.weekly,
								monthly: pruneRetention.monthly,
							})}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<div className="creation-dialog-fields" style={{ marginTop: 8 }}>
						<FormItem>
							<Label htmlFor="prune-daily">{t("backups.retentionDaily")}</Label>
							<Input
								id="prune-daily"
								inputMode="numeric"
								value={pruneDaily}
								onChange={(e) => setPruneDaily(e.target.value)}
							/>
						</FormItem>
						<FormItem>
							<Label htmlFor="prune-weekly">
								{t("backups.retentionWeekly")}
							</Label>
							<Input
								id="prune-weekly"
								inputMode="numeric"
								value={pruneWeekly}
								onChange={(e) => setPruneWeekly(e.target.value)}
							/>
						</FormItem>
						<FormItem>
							<Label htmlFor="prune-monthly">
								{t("backups.retentionMonthly")}
							</Label>
							<Input
								id="prune-monthly"
								inputMode="numeric"
								value={pruneMonthly}
								onChange={(e) => setPruneMonthly(e.target.value)}
							/>
						</FormItem>
					</div>
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={prune.isPending}
							onClick={(e) => {
								e.preventDefault();
								prune.mutate();
							}}
						>
							{prune.isPending
								? t("backups.pruning")
								: t("backups.confirmPrune")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>

			<AlertDialog
				open={confirmDelete !== null}
				onOpenChange={(open) => {
					if (!open) setConfirmDelete(null);
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>
							{t("backups.deleteConfirmTitle")}
						</AlertDialogTitle>
						<AlertDialogDescription>
							{t("backups.deleteConfirmDescription", {
								name: confirmDelete ?? "",
							})}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={remove.isPending}
							onClick={(e) => {
								e.preventDefault();
								if (confirmDelete) remove.mutate(confirmDelete);
							}}
						>
							{remove.isPending ? t("backups.deleting") : t("common.delete")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>

			<AlertDialog
				open={confirmRestore !== null}
				onOpenChange={(open) => {
					if (!open) setConfirmRestore(null);
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>
							{t("backups.restoreConfirmTitle")}
						</AlertDialogTitle>
						<AlertDialogDescription>
							{t("backups.restoreConfirmDescription")
								.split("{name}")
								.map((part, i, arr) => (
									// static two-part split of a translated template; the
									// interpolated archive name is rendered between them
									<span key={part}>
										{part}
										{i === 0 && arr.length > 1 ? (
											<span className="mono">{confirmRestore}</span>
										) : null}
									</span>
								))}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={restore.isPending}
							onClick={(e) => {
								e.preventDefault();
								if (confirmRestore) restore.mutate(confirmRestore);
							}}
						>
							{restore.isPending
								? t("backups.restoring")
								: t("backups.confirmRestore")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</>
	);
}
