import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch, mutationErrorMessage } from "../api/fetcher";
import type {
	BackupArchive,
	BackupSftpDestination,
	BackupSftpPutRequest,
} from "../api/generated/models";
import { useI18n } from "../i18n/I18nContext";
import { fmtBytes } from "../lib/bytes";
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "./ui/alert-dialog";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { FormItem, FormMessage } from "./ui/form";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Select } from "./ui/select";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "./ui/table";

type AuthType = "key" | "password";

interface SftpForm {
	enabled: boolean;
	host: string;
	port: string;
	user: string;
	remoteDir: string;
	authType: AuthType;
	keyPath: string;
	// Secret fields are write-only: blank means "keep the stored value",
	// the paired clear checkbox sends "" to drop it entirely.
	keyPassphrase: string;
	clearKeyPassphrase: boolean;
	password: string;
	clearPassword: boolean;
	hostKey: string;
	clearHostKey: boolean;
}

const emptyForm: SftpForm = {
	enabled: false,
	host: "",
	port: "22",
	user: "",
	remoteDir: "",
	authType: "key",
	keyPath: "",
	keyPassphrase: "",
	clearKeyPassphrase: false,
	password: "",
	clearPassword: false,
	hostKey: "",
	clearHostKey: false,
};

function formFromDestination(d: BackupSftpDestination): SftpForm {
	return {
		...emptyForm,
		enabled: d.enabled,
		host: d.host ?? "",
		port: d.port && d.port > 0 ? String(d.port) : "22",
		user: d.user ?? "",
		remoteDir: d.remoteDir ?? "",
		authType: d.authType === "password" ? "password" : "key",
		keyPath: d.keyPath ?? "",
	};
}

function toPutRequest(f: SftpForm): BackupSftpPutRequest {
	const port = Number.parseInt(f.port, 10);
	const body: BackupSftpPutRequest = {
		enabled: f.enabled,
		host: f.host.trim(),
		user: f.user.trim(),
		remoteDir: f.remoteDir.trim(),
		authType: f.authType,
	};
	// The API treats port 0/omitted as the SSH default 22.
	if (Number.isFinite(port) && port > 0 && port !== 22) body.port = port;
	if (f.authType === "key") {
		if (f.keyPath.trim()) body.keyPath = f.keyPath.trim();
		if (f.clearKeyPassphrase) body.keyPassphrase = "";
		else if (f.keyPassphrase) body.keyPassphrase = f.keyPassphrase;
	} else {
		if (f.clearPassword) body.password = "";
		else if (f.password) body.password = f.password;
	}
	if (f.clearHostKey) body.hostKey = "";
	else if (f.hostKey.trim()) body.hostKey = f.hostKey.trim();
	return body;
}

function formatStatusTimestamp(value?: string): string | null {
	if (!value) return null;
	const d = new Date(value);
	return Number.isNaN(d.getTime()) ? null : d.toLocaleString();
}

/** #1174: SFTP remote backup destination — configure the target, inspect the
 * last upload/fetch status, list remote archives, and fetch one back into the
 * local backup directory so it can be restored through the normal flow. */
export function BackupSftpPanel() {
	const { t } = useI18n();
	const qc = useQueryClient();
	const [editing, setEditing] = useState(false);
	const [form, setForm] = useState<SftpForm>(emptyForm);
	const [confirmRemove, setConfirmRemove] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [notice, setNotice] = useState<string | null>(null);

	const dest = useQuery<BackupSftpDestination>({
		queryKey: ["backups-sftp"],
		queryFn: () => apiFetch("/api/backups/sftp"),
	});
	const configured = dest.data?.configured === true;

	const remote = useQuery<BackupArchive[]>({
		queryKey: ["backups-sftp-remote"],
		queryFn: () => apiFetch("/api/backups/sftp/remote"),
		// Listing a destination that is not configured fails with 400 —
		// only ask once the destination exists.
		enabled: configured,
	});

	const invalidate = () => {
		void qc.invalidateQueries({ queryKey: ["backups-sftp"] });
		void qc.invalidateQueries({ queryKey: ["backups-sftp-remote"] });
	};

	const save = useMutation({
		mutationFn: (body: BackupSftpPutRequest) =>
			apiFetch("/api/backups/sftp", {
				method: "PUT",
				body: JSON.stringify(body),
			}),
		onSuccess: () => {
			setEditing(false);
			setError(null);
			setNotice(t("backups.sftp.notice.saved"));
			invalidate();
		},
		onError: (e) =>
			setError(mutationErrorMessage(e, t("backups.sftp.error.save"), t)),
	});

	const remove = useMutation({
		mutationFn: () => apiFetch("/api/backups/sftp", { method: "DELETE" }),
		onSuccess: () => {
			setConfirmRemove(false);
			setError(null);
			setNotice(t("backups.sftp.notice.removed"));
			invalidate();
		},
		onError: (e) =>
			setError(mutationErrorMessage(e, t("backups.sftp.error.remove"), t)),
	});

	const fetchRemote = useMutation({
		mutationFn: (name: string) =>
			apiFetch<BackupArchive>("/api/backups/sftp/fetch", {
				method: "POST",
				body: JSON.stringify({ name }),
			}),
		onSuccess: (archive) => {
			setError(null);
			setNotice(t("backups.sftp.notice.fetched", { name: archive.name }));
			// The fetched archive is now a normal local backup — refresh both
			// tables so it shows up in the restore list immediately.
			void qc.invalidateQueries({ queryKey: ["backups"] });
			invalidate();
		},
		onError: (e) =>
			setError(mutationErrorMessage(e, t("backups.sftp.error.fetch"), t)),
	});

	const startEdit = () => {
		setError(null);
		setForm(dest.data?.configured ? formFromDestination(dest.data) : emptyForm);
		setEditing(true);
	};

	const status = dest.data?.status;
	const lastUploadAt = formatStatusTimestamp(status?.lastUploadAt);
	const lastFetchAt = formatStatusTimestamp(status?.lastFetchAt);
	const lastErrorAt = formatStatusTimestamp(status?.lastErrorAt);
	const remoteItems = Array.isArray(remote.data) ? remote.data : [];

	const secretBadge = (set?: boolean) =>
		set ? (
			<Badge variant="success">{t("backups.sftp.secretSet")}</Badge>
		) : (
			<Badge>{t("backups.sftp.secretUnset")}</Badge>
		);

	return (
		<div className="card">
			<div className="h-scroll" style={{ gap: 8 }}>
				<h2 style={{ margin: 0, fontSize: 15, flex: 1 }}>
					{t("backups.sftp.title")}
				</h2>
				{configured ? (
					<Badge variant={dest.data?.enabled ? "success" : "default"}>
						{dest.data?.enabled ? t("common.enabled") : t("common.disabled")}
					</Badge>
				) : null}
				{!editing ? (
					<>
						<Button size="sm" onClick={startEdit}>
							{configured ? t("common.edit") : t("backups.sftp.configure")}
						</Button>
						{configured ? (
							<Button
								size="sm"
								variant="danger"
								onClick={() => {
									setError(null);
									setConfirmRemove(true);
								}}
							>
								{t("backups.sftp.remove")}
							</Button>
						) : null}
					</>
				) : null}
			</div>
			<p className="muted" style={{ fontSize: 12 }}>
				{t("backups.sftp.hint")}
			</p>
			{notice ? <p className="muted">{notice}</p> : null}
			{error ? <FormMessage>{error}</FormMessage> : null}

			{dest.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : dest.isError ? (
				<FormMessage>
					{dest.error instanceof ApiError
						? dest.error.message
						: t("backups.sftp.error.load")}
				</FormMessage>
			) : editing ? (
				<div className="form-stack" style={{ marginTop: 8 }}>
					<FormItem>
						<Label htmlFor="sftp-enabled">{t("common.enabled")}</Label>
						<input
							id="sftp-enabled"
							type="checkbox"
							checked={form.enabled}
							onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
							style={{ width: 18, height: 18 }}
						/>
					</FormItem>
					<FormItem>
						<Label htmlFor="sftp-host">{t("backups.sftp.host")}</Label>
						<Input
							id="sftp-host"
							value={form.host}
							onChange={(e) => setForm({ ...form, host: e.target.value })}
						/>
					</FormItem>
					<FormItem>
						<Label htmlFor="sftp-port">{t("backups.sftp.port")}</Label>
						<Input
							id="sftp-port"
							inputMode="numeric"
							placeholder="22"
							value={form.port}
							onChange={(e) => setForm({ ...form, port: e.target.value })}
						/>
					</FormItem>
					<FormItem>
						<Label htmlFor="sftp-user">{t("backups.sftp.user")}</Label>
						<Input
							id="sftp-user"
							value={form.user}
							onChange={(e) => setForm({ ...form, user: e.target.value })}
						/>
					</FormItem>
					<FormItem>
						<Label htmlFor="sftp-dir">{t("backups.sftp.remoteDir")}</Label>
						<Input
							id="sftp-dir"
							value={form.remoteDir}
							onChange={(e) => setForm({ ...form, remoteDir: e.target.value })}
						/>
					</FormItem>
					<FormItem>
						<Label htmlFor="sftp-auth">{t("backups.sftp.authType")}</Label>
						<Select
							id="sftp-auth"
							value={form.authType}
							onChange={(e) =>
								setForm({
									...form,
									authType: e.target.value === "password" ? "password" : "key",
								})
							}
						>
							<option value="key">{t("backups.sftp.authKey")}</option>
							<option value="password">{t("backups.sftp.authPassword")}</option>
						</Select>
					</FormItem>
					{form.authType === "key" ? (
						<>
							<FormItem>
								<Label htmlFor="sftp-keypath">
									{t("backups.sftp.keyPath")}
								</Label>
								<Input
									id="sftp-keypath"
									placeholder={t("backups.sftp.keyPathPlaceholder")}
									value={form.keyPath}
									onChange={(e) =>
										setForm({ ...form, keyPath: e.target.value })
									}
								/>
							</FormItem>
							<FormItem>
								<Label htmlFor="sftp-keypass">
									{t("backups.sftp.keyPassphrase")}
								</Label>
								<Input
									id="sftp-keypass"
									type="password"
									autoComplete="new-password"
									placeholder={
										dest.data?.keyPassphraseSet
											? t("backups.sftp.keepSecret")
											: undefined
									}
									value={form.keyPassphrase}
									onChange={(e) =>
										setForm({
											...form,
											keyPassphrase: e.target.value,
											clearKeyPassphrase: false,
										})
									}
								/>
								{secretBadge(dest.data?.keyPassphraseSet)}
								{dest.data?.keyPassphraseSet ? (
									<label className="muted" style={{ fontSize: 12 }}>
										<input
											type="checkbox"
											checked={form.clearKeyPassphrase}
											onChange={(e) =>
												setForm({
													...form,
													clearKeyPassphrase: e.target.checked,
												})
											}
										/>{" "}
										{t("backups.sftp.clearSecret")}
									</label>
								) : null}
							</FormItem>
						</>
					) : (
						<FormItem>
							<Label htmlFor="sftp-password">
								{t("backups.sftp.password")}
							</Label>
							<Input
								id="sftp-password"
								type="password"
								autoComplete="new-password"
								placeholder={
									dest.data?.passwordSet
										? t("backups.sftp.keepSecret")
										: undefined
								}
								value={form.password}
								onChange={(e) =>
									setForm({
										...form,
										password: e.target.value,
										clearPassword: false,
									})
								}
							/>
							{secretBadge(dest.data?.passwordSet)}
							{dest.data?.passwordSet ? (
								<label className="muted" style={{ fontSize: 12 }}>
									<input
										type="checkbox"
										checked={form.clearPassword}
										onChange={(e) =>
											setForm({
												...form,
												clearPassword: e.target.checked,
											})
										}
									/>{" "}
									{t("backups.sftp.clearSecret")}
								</label>
							) : null}
						</FormItem>
					)}
					<FormItem>
						<Label htmlFor="sftp-hostkey">{t("backups.sftp.hostKey")}</Label>
						<Input
							id="sftp-hostkey"
							placeholder={t("backups.sftp.hostKeyPlaceholder")}
							value={form.hostKey}
							onChange={(e) =>
								setForm({
									...form,
									hostKey: e.target.value,
									clearHostKey: false,
								})
							}
						/>
						<p className="muted" style={{ fontSize: 12 }}>
							{t("backups.sftp.hostKeyHint")}
						</p>
						{secretBadge(dest.data?.hostKeySet)}
						{dest.data?.hostKeySet ? (
							<label className="muted" style={{ fontSize: 12 }}>
								<input
									type="checkbox"
									checked={form.clearHostKey}
									onChange={(e) =>
										setForm({
											...form,
											clearHostKey: e.target.checked,
										})
									}
								/>{" "}
								{t("backups.sftp.clearSecret")}
							</label>
						) : null}
					</FormItem>
					<div style={{ display: "flex", gap: 8, marginTop: 4 }}>
						<Button
							variant="primary"
							disabled={save.isPending}
							onClick={() => {
								setError(null);
								save.mutate(toPutRequest(form));
							}}
						>
							{save.isPending ? t("backups.sftp.saving") : t("common.save")}
						</Button>
						<Button onClick={() => setEditing(false)}>
							{t("common.cancel")}
						</Button>
					</div>
				</div>
			) : configured ? (
				<>
					<p className="mono" style={{ fontSize: 13 }}>
						{dest.data?.user}@{dest.data?.host}:
						{dest.data?.port && dest.data.port > 0 ? dest.data.port : 22}
						{dest.data?.remoteDir}
					</p>
					<p className="muted" style={{ fontSize: 12 }}>
						{t("backups.sftp.summary", {
							auth:
								dest.data?.authType === "password"
									? t("backups.sftp.authPassword")
									: t("backups.sftp.authKey"),
						})}
					</p>
					{status && (lastUploadAt || lastFetchAt || status.lastError) ? (
						<div className="muted" style={{ fontSize: 12 }}>
							{lastUploadAt ? (
								<p style={{ margin: "2px 0" }}>
									{t("backups.sftp.status.lastUpload", {
										at: lastUploadAt,
										name: status.lastUploadArchive ?? "",
									})}
								</p>
							) : null}
							{lastFetchAt ? (
								<p style={{ margin: "2px 0" }}>
									{t("backups.sftp.status.lastFetch", {
										at: lastFetchAt,
										name: status.lastFetchArchive ?? "",
									})}
								</p>
							) : null}
							{status.lastError ? (
								<p style={{ margin: "2px 0" }}>
									{t("backups.sftp.status.lastError", {
										at: lastErrorAt ?? "",
										error: status.lastError,
									})}
								</p>
							) : null}
						</div>
					) : null}

					<h3 style={{ fontSize: 14, marginTop: 12 }}>
						{t("backups.sftp.remoteTitle")}
					</h3>
					{remote.isLoading ? (
						<p className="muted">{t("common.loading")}</p>
					) : remote.isError ? (
						<FormMessage>
							{remote.error instanceof ApiError
								? remote.error.message
								: t("backups.sftp.error.remoteList")}
						</FormMessage>
					) : (
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>{t("common.name")}</TableHead>
									<TableHead>{t("backups.size")}</TableHead>
									<TableHead>{t("common.created")}</TableHead>
									<TableHead>{t("common.actions")}</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{remoteItems.length === 0 ? (
									<TableRow>
										<TableCell colSpan={4} className="muted">
											{t("backups.sftp.remoteEmpty")}
										</TableCell>
									</TableRow>
								) : (
									remoteItems.map((b) => (
										<TableRow key={b.name}>
											<TableCell className="mono">{b.name}</TableCell>
											<TableCell className="muted">
												{fmtBytes(b.size)}
											</TableCell>
											<TableCell className="muted">
												{new Date(b.createdAt).toLocaleString()}
											</TableCell>
											<TableCell>
												{b.encrypted ? (
													<Button
														size="sm"
														disabled={fetchRemote.isPending}
														onClick={() => {
															setError(null);
															fetchRemote.mutate(b.name);
														}}
													>
														{t("backups.sftp.fetch")}
													</Button>
												) : (
													<span className="muted" style={{ fontSize: 12 }}>
														{t("backups.sftp.notEncrypted")}
													</span>
												)}
											</TableCell>
										</TableRow>
									))
								)}
							</TableBody>
						</Table>
					)}
					<p className="muted" style={{ fontSize: 12 }}>
						{t("backups.sftp.fetchHint")}
					</p>
				</>
			) : (
				<p className="muted">{t("backups.sftp.notConfigured")}</p>
			)}

			<AlertDialog
				open={confirmRemove}
				onOpenChange={(open) => {
					if (!open) setConfirmRemove(false);
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>
							{t("backups.sftp.removeConfirmTitle")}
						</AlertDialogTitle>
						<AlertDialogDescription>
							{t("backups.sftp.removeConfirmDescription")}
						</AlertDialogDescription>
					</AlertDialogHeader>
					{error ? <FormMessage>{error}</FormMessage> : null}
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={remove.isPending}
							onClick={(e) => {
								e.preventDefault();
								remove.mutate();
							}}
						>
							{remove.isPending
								? t("backups.sftp.removing")
								: t("common.delete")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</div>
	);
}
