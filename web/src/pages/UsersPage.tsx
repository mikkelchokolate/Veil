import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { apiFetch, ApiError } from "../api/fetcher";
import { useAuth, useIsAdmin } from "../auth/AuthContext";
import type { UserResponse } from "../api/generated/models";

type PanelUser = UserResponse;

/** Panel users (RBAC): list + create admin/viewer. Self-deletion guarded
 * server-side (admin invariant). */
export function UsersPage() {
	const isAdmin = useIsAdmin();
	const { session } = useAuth();
	const qc = useQueryClient();
	const [username, setUsername] = useState("");
	const [password, setPassword] = useState("");
	const [role, setRole] = useState<"admin" | "viewer">("viewer");
	const [error, setError] = useState<string | null>(null);

	const users = useQuery<PanelUser[]>({
		queryKey: ["users"],
		queryFn: () => apiFetch("/api/users"),
	});

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
			void qc.invalidateQueries({ queryKey: ["users"] });
		},
		onError: (err) => setError(err instanceof ApiError ? err.message : "Create failed"),
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
											<span className={`badge${u.role === "admin" ? " badge-success" : ""}`}>
												{u.role}
											</span>
										</td>
										<td className="muted">{u.locale ?? "en"}</td>
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
					{error ? <p className="form-error">{error}</p> : null}
					<button type="submit" className="btn btn-primary" disabled={create.isPending}>
						{create.isPending ? "Creating…" : "Create user"}
					</button>
				</form>
			</div>
		</>
	);
}
