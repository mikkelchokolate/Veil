import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import type { BackupArchive } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";
import { fmtBytes } from "../lib/bytes";

interface RestoreJob {
	id: string;
	archive: string;
	status: string;
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
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [error, setError] = useState<string | null>(null);
	const [notice, setNotice] = useState<string | null>(null);
	const [verifyResult, setVerifyResult] = useState<
		Record<string, VerifyResult>
	>({});
	const [confirmRestore, setConfirmRestore] = useState<string | null>(null);
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
		queryFn: () => apiFetch(`/api/backup-restore-jobs/${activeJob?.id}`),
		enabled:
			!!activeJob &&
			(activeJob.status === "queued" || activeJob.status === "running"),
		refetchInterval: 1500,
	});
	const job = jobQuery.data ?? activeJob;

	const create = useMutation({
		mutationFn: () =>
			apiFetch("/api/backups", { method: "POST", body: JSON.stringify({}) }),
		onSuccess: () => {
			setError(null);
			setNotice("Backup created.");
			void qc.invalidateQueries({ queryKey: ["backups"] });
		},
		onError: (e) =>
			setError(e instanceof ApiError ? e.message : "Backup failed"),
	});

	const prune = useMutation({
		mutationFn: () =>
			apiFetch("/api/backups/prune", {
				method: "POST",
				body: JSON.stringify({}),
			}),
		onSuccess: () => {
			setError(null);
			setNotice("Retention prune complete.");
			void qc.invalidateQueries({ queryKey: ["backups"] });
		},
		onError: (e) =>
			setError(e instanceof ApiError ? e.message : "Prune failed"),
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
			return { name, res: res as VerifyResult };
		},
		onSuccess: ({ name, res }) => {
			setError(null);
			setVerifyResult((prev) => ({ ...prev, [name]: res }));
		},
		onError: (e) =>
			setError(e instanceof ApiError ? e.message : "Verify failed"),
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
			setNotice(
				`Restore queued for ${j.archive}. You may be logged out when it finishes.`,
			);
			setActiveJob(j);
		},
		onError: (e) =>
			setError(e instanceof ApiError ? e.message : "Restore failed"),
	});

	async function download(name: string) {
		try {
			const res = await fetch(
				`/api/backups/${encodeURIComponent(name)}/download`,
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
			setError(e instanceof Error ? e.message : "Download failed");
		}
	}

	return (
		<>
			<div className="card">
				<div style={{ display: "flex", gap: 8, alignItems: "center" }}>
					<h2 style={{ margin: 0, flex: 1 }}>Backups</h2>
					{isAdmin ? (
						<>
							<button
								type="button"
								className="btn"
								disabled={prune.isPending}
								onClick={() => prune.mutate()}
							>
								{prune.isPending ? "Pruning…" : "Prune (retention)"}
							</button>
							<button
								type="button"
								className="btn btn-primary"
								disabled={create.isPending}
								onClick={() => create.mutate()}
							>
								{create.isPending ? "Creating…" : "Create backup"}
							</button>
						</>
					) : null}
				</div>
				{notice ? <p className="muted">{notice}</p> : null}
				{error ? <p className="form-error">{error}</p> : null}
			</div>

			{job ? (
				<div className="card">
					<h2 style={{ fontSize: 15 }}>Restore job</h2>
					<p>
						<span
							className={`badge${
								job.status === "finished"
									? " badge-success"
									: job.status === "failed"
										? " badge-danger"
										: " badge-warning"
							}`}
						>
							{job.status}
						</span>{" "}
						<span className="mono">{job.archive}</span>
					</p>
					{job.error ? <p className="form-error">{job.error}</p> : null}
					{job.status === "finished" || job.status === "failed" ? (
						<button
							type="button"
							className="btn"
							onClick={() => {
								setActiveJob(null);
								void qc.invalidateQueries({ queryKey: ["backups"] });
							}}
						>
							Dismiss
						</button>
					) : null}
				</div>
			) : null}

			<div className="card">
				{backups.isLoading ? (
					<p className="muted">Loading…</p>
				) : (
					<div className="table-container">
						<table className="data-table">
							<thead>
								<tr>
									<th>Name</th>
									<th>Size</th>
									<th>Created</th>
									<th>Encrypted</th>
									<th>Verify</th>
									{isAdmin ? <th>Actions</th> : null}
								</tr>
							</thead>
							<tbody>
								{items.length === 0 ? (
									<tr>
										<td colSpan={6} className="muted">
											No backups yet.
										</td>
									</tr>
								) : (
									items.map((b) => {
										const v = verifyResult[b.name];
										return (
											<tr key={b.name}>
												<td className="mono">{b.name}</td>
												<td className="muted">{fmtBytes(b.size)}</td>
												<td className="muted">
													{new Date(b.createdAt).toLocaleString()}
												</td>
												<td>
													<span
														className={`badge${b.encrypted ? " badge-success" : ""}`}
													>
														{b.encrypted ? "yes" : "no"}
													</span>
												</td>
												<td>
													{v ? (
														<span
															className={`badge${
																v.ok || v.valid
																	? " badge-success"
																	: " badge-danger"
															}`}
														>
															{v.ok || v.valid
																? "ok"
																: (v.error ?? v.message ?? "invalid")}
														</span>
													) : (
														<span className="muted">—</span>
													)}
												</td>
												{isAdmin ? (
													<td>
														<div
															style={{
																display: "flex",
																gap: 6,
																flexWrap: "wrap",
															}}
														>
															<button
																type="button"
																className="btn"
																onClick={() => void download(b.name)}
															>
																Download
															</button>
															<button
																type="button"
																className="btn"
																disabled={verify.isPending}
																onClick={() => verify.mutate(b.name)}
															>
																Verify
															</button>
															{confirmRestore === b.name ? (
																<>
																	<button
																		type="button"
																		className="btn btn-danger"
																		disabled={restore.isPending}
																		onClick={() => restore.mutate(b.name)}
																	>
																		Confirm restore
																	</button>
																	<button
																		type="button"
																		className="btn"
																		onClick={() => setConfirmRestore(null)}
																	>
																		Cancel
																	</button>
																</>
															) : (
																<button
																	type="button"
																	className="btn btn-danger"
																	onClick={() => setConfirmRestore(b.name)}
																>
																	Restore
																</button>
															)}
														</div>
													</td>
												) : null}
											</tr>
										);
									})
								)}
							</tbody>
						</table>
					</div>
				)}
			</div>
		</>
	);
}
