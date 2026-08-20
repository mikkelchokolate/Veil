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
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";
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
import { useI18n } from "../i18n/I18nContext";

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
	const { t } = useI18n();
	const isAdmin = useIsAdmin();
	const { session } = useAuth();
	const qc = useQueryClient();
	const [creatingUser, setCreatingUser] = useState(false);
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
			setCreatingUser(false);
			setError(null);
			setNotice(t("users.createdNotice", { name: username }));
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : t("users.error.create")),
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
			setNotice(t("users.updatedNotice", { name: args.name }));
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : t("users.error.update")),
	});

	const remove = useMutation({
		mutationFn: (name: string) =>
			apiFetch(`/api/users/${encodeURIComponent(name)}`, { method: "DELETE" }),
		onSuccess: (_d, name) => {
			setConfirmDelete(null);
			setError(null);
			setNotice(t("users.deletedNotice", { name }));
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : t("users.error.delete")),
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
			setError(err instanceof ApiError ? err.message : t("users.error.revoke")),
	});

	function onSubmit(e: FormEvent) {
		e.preventDefault();
		create.mutate();
	}

	if (!isAdmin) {
		return (
			<div className="card">
				<h2>{t("users.title")}</h2>
				<p className="muted">{t("users.adminRequired")}</p>
			</div>
		);
	}

	return (
		<>
			<div className="card">
				<div style={{ display: "flex", gap: 12, alignItems: "center" }}>
					<h2 style={{ margin: 0, flex: 1 }}>{t("users.panelUsers")}</h2>
					<Button
						variant="primary"
						onClick={() => {
							setError(null);
							setCreatingUser(true);
						}}
					>
						{t("users.createUser")}
					</Button>
				</div>
				{notice ? <p className="muted">{notice}</p> : null}
				{error ? <FormMessage>{error}</FormMessage> : null}
				{users.isLoading ? (
					<p className="muted">{t("common.loading")}</p>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>{t("users.username")}</TableHead>
								<TableHead>{t("users.role")}</TableHead>
								<TableHead>{t("users.locale")}</TableHead>
								<TableHead>{t("common.actions")}</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{(users.data ?? []).map((u) => (
								<TableRow key={u.username}>
									<TableCell>
										{u.username}
										{session?.username === u.username ? (
											<span className="muted">{t("users.you")}</span>
										) : null}
									</TableCell>
									<TableCell>
										<Badge variant={u.role === "admin" ? "success" : "default"}>
											{t(`users.role.${u.role}`)}
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
												{t("common.edit")}
											</Button>
											{session?.username !== u.username ? (
												<Button
													size="sm"
													variant="danger"
													onClick={() => setConfirmDelete(u.username)}
												>
													{t("common.delete")}
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
													aria-label={t("users.role")}
												>
													<option value="viewer">
														{t("users.role.viewer")}
													</option>
													<option value="admin">{t("users.role.admin")}</option>
												</Select>
												<Input
													style={{ maxWidth: 180 }}
													type="password"
													placeholder={t("users.newPasswordOptional")}
													minLength={12}
													value={editPassword}
													onChange={(e) => setEditPassword(e.target.value)}
													aria-label={t("users.newPassword")}
												/>
												<Button
													type="submit"
													variant="primary"
													disabled={update.isPending}
												>
													{t("common.save")}
												</Button>
												<Button onClick={() => setEditing(null)}>
													{t("common.cancel")}
												</Button>
											</form>
										) : null}
									</TableCell>
								</TableRow>
							))}
						</TableBody>
					</Table>
				)}
			</div>

			<Dialog open={creatingUser} onOpenChange={setCreatingUser}>
				<DialogContent className="creation-dialog">
					<div className="card">
						<DialogTitle>{t("users.createUser")}</DialogTitle>
						<form onSubmit={onSubmit}>
							<FormItem>
								<Label htmlFor="nu-username">{t("auth.username")}</Label>
								<Input
									id="nu-username"
									value={username}
									onChange={(e) => setUsername(e.target.value)}
									required
								/>
							</FormItem>
							<FormItem>
								<Label htmlFor="nu-password">{t("auth.password")}</Label>
								<Input
									id="nu-password"
									type="password"
									minLength={12}
									value={password}
									onChange={(e) => setPassword(e.target.value)}
									required
								/>
							</FormItem>
							<FormItem>
								<Label htmlFor="nu-role">{t("users.role")}</Label>
								<Select
									id="nu-role"
									value={role}
									onChange={(e) =>
										setRole(e.target.value as "admin" | "viewer")
									}
								>
									<option value="viewer">
										{t("users.role.viewerReadOnly")}
									</option>
									<option value="admin">{t("users.role.admin")}</option>
								</Select>
							</FormItem>
							{error ? <FormMessage>{error}</FormMessage> : null}
							<div className="creation-dialog-actions">
								<Button type="button" onClick={() => setCreatingUser(false)}>
									{t("common.cancel")}
								</Button>
								<Button
									type="submit"
									variant="primary"
									disabled={create.isPending}
								>
									{create.isPending
										? t("users.creating")
										: t("users.createUser")}
								</Button>
							</div>
						</form>
					</div>
				</DialogContent>
			</Dialog>

			<div className="card">
				<h2>{t("users.activeSessions")}</h2>
				{sessions.isLoading ? (
					<p className="muted">{t("common.loading")}</p>
				) : (sessions.data ?? []).length === 0 ? (
					<p className="muted">{t("users.noActiveSessions")}</p>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>{t("users.sessionUser")}</TableHead>
								<TableHead>{t("users.role")}</TableHead>
								<TableHead>{t("users.lastSeen")}</TableHead>
								<TableHead>{t("users.expires")}</TableHead>
								<TableHead>{t("users.agent")}</TableHead>
								<TableHead />
							</TableRow>
						</TableHeader>
						<TableBody>
							{(sessions.data ?? []).map((s) => (
								<TableRow key={s.id}>
									<TableCell>
										{s.username}
										{s.current ? (
											<span className="muted">{t("users.thisSession")}</span>
										) : null}
									</TableCell>
									<TableCell className="muted">
										{t(`users.role.${s.role}`)}
									</TableCell>
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
												{t("users.revoke")}
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
						<AlertDialogTitle>{t("users.deleteDialogTitle")}</AlertDialogTitle>
						<AlertDialogDescription>
							{t("users.deleteDialogDescription", {
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
							{remove.isPending
								? t("users.deleting")
								: t("users.confirmDelete")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</>
	);
}
