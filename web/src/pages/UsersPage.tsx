import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import type { UserResponse } from "../api/generated/models";
import { useAuth, useIsAdmin } from "../auth/AuthContext";
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
import { Select } from "../components/ui/select";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "../components/ui/table";

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
				{error ? <FormMessage>{error}</FormMessage> : null}
				{users.isLoading ? (
					<p className="muted">Loading…</p>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Username</TableHead>
								<TableHead>Role</TableHead>
								<TableHead>Locale</TableHead>
								<TableHead>Actions</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{(users.data ?? []).map((u) => (
								<TableRow key={u.username}>
									<TableCell>
										{u.username}
										{session?.username === u.username ? (
											<span className="muted"> (you)</span>
										) : null}
									</TableCell>
									<TableCell>
										<Badge variant={u.role === "admin" ? "success" : "default"}>
											{u.role}
										</Badge>
									</TableCell>
									<TableCell className="muted">{u.locale ?? "en"}</TableCell>
									<TableCell>
										<div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
											<Button
												size="sm"
												onClick={() => {
													setEditing(u.username);
													setEditRole(
														(u.role as "admin" | "viewer") ?? "viewer",
													);
													setEditPassword("");
												}}
											>
												Edit
											</Button>
											{session?.username !== u.username ? (
												<Button
													size="sm"
													variant="danger"
													onClick={() => setConfirmDelete(u.username)}
												>
													Delete
												</Button>
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
												<Select
													style={{ maxWidth: 130 }}
													value={editRole}
													onChange={(e) =>
														setEditRole(e.target.value as "admin" | "viewer")
													}
													aria-label="role"
												>
													<option value="viewer">viewer</option>
													<option value="admin">admin</option>
												</Select>
												<Input
													style={{ maxWidth: 180 }}
													type="password"
													placeholder="New password (optional)"
													value={editPassword}
													onChange={(e) => setEditPassword(e.target.value)}
													aria-label="new password"
												/>
												<Button
													type="submit"
													variant="primary"
													disabled={update.isPending}
												>
													Save
												</Button>
												<Button onClick={() => setEditing(null)}>Cancel</Button>
											</form>
										) : null}
									</TableCell>
								</TableRow>
							))}
						</TableBody>
					</Table>
				)}
			</div>

			<div className="card">
				<h2>Create user</h2>
				<form onSubmit={onSubmit}>
					<FormItem>
						<Label htmlFor="nu-username">Username</Label>
						<Input
							id="nu-username"
							value={username}
							onChange={(e) => setUsername(e.target.value)}
							required
						/>
					</FormItem>
					<FormItem>
						<Label htmlFor="nu-password">Password</Label>
						<Input
							id="nu-password"
							type="password"
							value={password}
							onChange={(e) => setPassword(e.target.value)}
							required
						/>
					</FormItem>
					<FormItem>
						<Label htmlFor="nu-role">Role</Label>
						<Select
							id="nu-role"
							value={role}
							onChange={(e) => setRole(e.target.value as "admin" | "viewer")}
						>
							<option value="viewer">viewer (read-only)</option>
							<option value="admin">admin</option>
						</Select>
					</FormItem>
					<Button type="submit" variant="primary" disabled={create.isPending}>
						{create.isPending ? "Creating…" : "Create user"}
					</Button>
				</form>
			</div>

			<div className="card">
				<h2>Active sessions</h2>
				{sessions.isLoading ? (
					<p className="muted">Loading…</p>
				) : (sessions.data ?? []).length === 0 ? (
					<p className="muted">No active sessions.</p>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>User</TableHead>
								<TableHead>Role</TableHead>
								<TableHead>Last seen</TableHead>
								<TableHead>Expires</TableHead>
								<TableHead>Agent</TableHead>
								<TableHead />
							</TableRow>
						</TableHeader>
						<TableBody>
							{(sessions.data ?? []).map((s) => (
								<TableRow key={s.id}>
									<TableCell>
										{s.username}
										{s.current ? <span className="muted"> (this)</span> : null}
									</TableCell>
									<TableCell className="muted">{s.role}</TableCell>
									<TableCell className="muted">
										{new Date(s.lastSeenAt).toLocaleString()}
									</TableCell>
									<TableCell className="muted">
										{new Date(s.expiresAt).toLocaleString()}
									</TableCell>
									<TableCell
										className="muted"
										style={{
											maxWidth: 200,
											overflow: "hidden",
											textOverflow: "ellipsis",
										}}
									>
										{s.userAgent ?? "—"}
									</TableCell>
									<TableCell>
										{!s.current ? (
											<Button
												size="sm"
												disabled={revoke.isPending}
												onClick={() => revoke.mutate(s.id)}
											>
												Revoke
											</Button>
										) : null}
									</TableCell>
								</TableRow>
							))}
						</TableBody>
					</Table>
				)}
			</div>

			<AlertDialog
				open={confirmDelete !== null}
				onOpenChange={(open) => {
					if (!open) setConfirmDelete(null);
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>Delete user?</AlertDialogTitle>
						<AlertDialogDescription>
							Deleting <span className="mono">{confirmDelete}</span> removes the
							account and revokes its sessions. This cannot be undone.
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel>Cancel</AlertDialogCancel>
						<AlertDialogAction
							disabled={remove.isPending}
							onClick={(e) => {
								e.preventDefault();
								if (confirmDelete) remove.mutate(confirmDelete);
							}}
						>
							{remove.isPending ? "Deleting…" : "Confirm delete"}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</>
	);
}
