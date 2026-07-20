import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import type { UserResponse } from "../api/generated/models";
import { useAuth, useIsAdmin } from "../auth/AuthContext";

type PanelUser = UserResponse;

interface SessionInfo {
	id: string;
	username: string;
	role: string;
	createdAt: string;
	lastSeenAt: string;
	expiresAt: string;
	userAgent?: string;
	remoteAddr?: string;
	current: boolean;
}

/** S4: full panel user management — create, edit role/password, delete (with
 * confirm), and active-session listing + revocation. */
export function UsersPage() {
	const isAdmin = useIsAdmin();
	const { session } = useAuth();
	const qc = useQueryClient();
	const [username, setUsername] = useState("");
	const [password, setPassword] = useState("");
	const [role, setRole] = useState<"admin" | "viewer">("viewer");
	const [error, setError] = useState<string | null>(null);
	const [notice, setNotice] = useState<string | null>(null);
	const [editing, setEditing] = useState<string | null>(null);
	const [editRole, setEditRole] = useState<"admin" | "viewer">("viewer");
	const [editPassword, setEditPassword] = useState("");
	const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

	const users = useQuery<PanelUser[]>({
		queryKey: ["users"],
		queryFn: () => apiFetch("/api/users"),
	});
	const sessions = useQuery<SessionInfo[]>({
		queryKey: ["auth-sessions"],
		queryFn: () => apiFetch("/api/auth/sessions"),
	});

	function invalidate() {
		void qc.invalidateQueries({ queryKey: ["users"] });
		void qc.invalidateQueries({ queryKey: ["auth-sessions"] });
	}

	const create = useMutation({
		mutationFn: () =>
			apiFetch("/api/users", {
				method: "POST",
				body: JSON.stringify({ username, password, role }),
			}),
		onSuccess: () => {
			setUsername("");
			setPassword("");
			setError(null);
			setNotice(`User ${username} created.`);
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Create failed"),
	});

	const update = useMutation({
		mutationFn: (args: {
			name: string;
			role: string;
			password?: string | undefined;
		}) =>
			apiFetch(`/api/users/${encodeURIComponent(args.name)}`, {
				method: "PUT",
				body: JSON.stringify({
					role: args.role,
					...(args.password ? { password: args.password } : {}),
				}),
			}),
		onSuccess: (_d, args) => {
			setEditing(null);
			setEditPassword("");
			setError(null);
			setNotice(`User ${args.name} updated.`);
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Update failed"),
	});

	const remove = useMutation({
		mutationFn: (name: string) =>
			apiFetch(`/api/users/${encodeURIComponent(name)}`, { method: "DELETE" }),
		onSuccess: (_d, name) => {
			setConfirmDelete(null);
			setError(null);
			setNotice(`User ${name} deleted.`);
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Delete failed"),
	});

	const revoke = useMutation({
		mutationFn: (id: string) =>
			apiFetch("/api/auth/sessions", {
				method: "DELETE",
				body: JSON.stringify({ id }),
			}),
		onSuccess: () => {
			setError(null);
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Revoke failed"),
	});

	function onSubmit(e: FormEvent) {
		e.preventDefault();
		create.mutate();
	}

	if (!isAdmin) {
		return (
			<div className="card">
				<h2>Users</h2>
				<p className="muted">User management requires the admin role.</p>
			</div>
		);
	}

	return (
		<>
			<div className="card">
				<h2>Panel users</h2>
				{notice ? <p className="muted">{notice}</p> : null}
				{error ? <p className="form-error">{error}</p> : null}
				{users.isLoading ? (
					<p className="muted">Loading…</p>
				) : (
					<div className="table-container">
						<table className="data-table">
							<thead>
								<tr>
									<th>Username</th>
									<th>Role</th>
									<th>Locale</th>
									<th>Actions</th>
								</tr>
							</thead>
							<tbody>
								{(users.data ?? []).map((u) => (
									<tr key={u.username}>
										<td>
											{u.username}
											{session?.username === u.username ? (
												<span className="muted"> (you)</span>
											) : null}
										</td>
										<td>
											<span
												className={`badge${u.role === "admin" ? " badge-success" : ""}`}
											>
												{u.role}
											</span>
										</td>
										<td className="muted">{u.locale ?? "en"}</td>
										<td>
											<div
												style={{ display: "flex", gap: 6, flexWrap: "wrap" }}
											>
												<button
													type="button"
													className="btn"
													onClick={() => {
														setEditing(u.username);
														setEditRole(
															(u.role as "admin" | "viewer") ?? "viewer",
														);
														setEditPassword("");
													}}
												>
													Edit
												</button>
												{session?.username !== u.username ? (
													confirmDelete === u.username ? (
														<>
															<button
																type="button"
																className="btn btn-danger"
																disabled={remove.isPending}
																onClick={() => remove.mutate(u.username)}
															>
																Confirm delete
															</button>
															<button
																type="button"
																className="btn"
																onClick={() => setConfirmDelete(null)}
															>
																Cancel
															</button>
														</>
													) : (
														<button
															type="button"
															className="btn btn-danger"
															onClick={() => setConfirmDelete(u.username)}
														>
															Delete
														</button>
													)
												) : null}
											</div>
											{editing === u.username ? (
												<form
													style={{
														marginTop: 8,
														display: "flex",
														gap: 8,
														flexWrap: "wrap",
														alignItems: "center",
													}}
													onSubmit={(e) => {
														e.preventDefault();
														update.mutate({
															name: u.username,
															role: editRole,
															password: editPassword || undefined,
														});
													}}
												>
													<select
														className="input"
														style={{ maxWidth: 130 }}
														value={editRole}
														onChange={(e) =>
															setEditRole(e.target.value as "admin" | "viewer")
														}
														aria-label="role"
													>
														<option value="viewer">viewer</option>
														<option value="admin">admin</option>
													</select>
													<input
														className="input"
														style={{ maxWidth: 180 }}
														type="password"
														placeholder="New password (optional)"
														value={editPassword}
														onChange={(e) => setEditPassword(e.target.value)}
														aria-label="new password"
													/>
													<button
														type="submit"
														className="btn btn-primary"
														disabled={update.isPending}
													>
														Save
													</button>
													<button
														type="button"
														className="btn"
														onClick={() => setEditing(null)}
													>
														Cancel
													</button>
												</form>
											) : null}
										</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				)}
			</div>

			<div className="card">
				<h2>Create user</h2>
				<form onSubmit={onSubmit}>
					<div className="form-group">
						<label htmlFor="nu-username">Username</label>
						<input
							id="nu-username"
							className="input"
							value={username}
							onChange={(e) => setUsername(e.target.value)}
							required
						/>
					</div>
					<div className="form-group">
						<label htmlFor="nu-password">Password</label>
						<input
							id="nu-password"
							className="input"
							type="password"
							value={password}
							onChange={(e) => setPassword(e.target.value)}
							required
						/>
					</div>
					<div className="form-group">
						<label htmlFor="nu-role">Role</label>
						<select
							id="nu-role"
							className="input"
							value={role}
							onChange={(e) => setRole(e.target.value as "admin" | "viewer")}
						>
							<option value="viewer">viewer (read-only)</option>
							<option value="admin">admin</option>
						</select>
					</div>
					<button
						type="submit"
						className="btn btn-primary"
						disabled={create.isPending}
					>
						{create.isPending ? "Creating…" : "Create user"}
					</button>
				</form>
			</div>

			<div className="card">
				<h2>Active sessions</h2>
				{sessions.isLoading ? (
					<p className="muted">Loading…</p>
				) : (sessions.data ?? []).length === 0 ? (
					<p className="muted">No active sessions.</p>
				) : (
					<div className="table-container">
						<table className="data-table">
							<thead>
								<tr>
									<th>User</th>
									<th>Role</th>
									<th>Last seen</th>
									<th>Expires</th>
									<th>Agent</th>
									<th></th>
								</tr>
							</thead>
							<tbody>
								{(sessions.data ?? []).map((s) => (
									<tr key={s.id}>
										<td>
											{s.username}
											{s.current ? (
												<span className="muted"> (this)</span>
											) : null}
										</td>
										<td className="muted">{s.role}</td>
										<td className="muted">
											{new Date(s.lastSeenAt).toLocaleString()}
										</td>
										<td className="muted">
											{new Date(s.expiresAt).toLocaleString()}
										</td>
										<td
											className="muted"
											style={{
												maxWidth: 200,
												overflow: "hidden",
												textOverflow: "ellipsis",
											}}
										>
											{s.userAgent ?? "—"}
										</td>
										<td>
											{!s.current ? (
												<button
													type="button"
													className="btn"
													disabled={revoke.isPending}
													onClick={() => revoke.mutate(s.id)}
												>
													Revoke
												</button>
											) : null}
										</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				)}
			</div>
		</>
	);
}
