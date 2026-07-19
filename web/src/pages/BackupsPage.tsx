import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch, ApiError } from "../api/fetcher";
import { useIsAdmin } from "../auth/AuthContext";

interface BackupArchive {
	name: string;
	path: string;
	size: number;
	createdAt: string;
	encrypted: boolean;
}

function fmtBytes(n: number): string {
	const units = ["B", "KiB", "MiB", "GiB"];
	let v = n;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[i]}`;
}

/** Backups: list archives + create. Retention and restore are server-side. */
export function BackupsPage() {
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();

	const backups = useQuery<{ items?: BackupArchive[] } | BackupArchive[]>({
		queryKey: ["backups"],
		queryFn: () => apiFetch("/api/backups"),
	});

	const create = useMutation({
		mutationFn: () => apiFetch("/api/backups", { method: "POST", body: JSON.stringify({}) }),
		onSuccess: () => void qc.invalidateQueries({ queryKey: ["backups"] }),
	});

	const items: BackupArchive[] = Array.isArray(backups.data)
		? backups.data
		: (backups.data?.items ?? []);

	return (
		<div className="card">
			<h2>Backups</h2>
			{isAdmin ? (
				<button
					type="button"
					className="btn btn-primary"
					disabled={create.isPending}
					onClick={() => create.mutate()}
				>
					{create.isPending ? "Creating…" : "Create backup"}
				</button>
			) : null}
			{create.isError ? (
				<p className="form-error">
					{create.error instanceof ApiError ? create.error.message : "Backup failed"}
				</p>
			) : null}
			{backups.isLoading ? (
				<p className="muted">Loading…</p>
			) : (
				<div className="table-container" style={{ marginTop: 12 }}>
					<table className="data-table">
						<thead>
							<tr>
								<th>Name</th>
								<th>Size</th>
								<th>Created</th>
								<th>Encrypted</th>
							</tr>
						</thead>
						<tbody>
							{items.length === 0 ? (
								<tr>
									<td colSpan={4} className="muted">
										No backups yet.
									</td>
								</tr>
							) : (
								items.map((b) => (
									<tr key={b.name}>
										<td className="mono">{b.name}</td>
										<td className="muted">{fmtBytes(b.size)}</td>
										<td className="muted">{new Date(b.createdAt).toLocaleString()}</td>
										<td>
											<span className={`badge${b.encrypted ? " badge-success" : ""}`}>
												{b.encrypted ? "yes" : "no"}
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
