import {
	browserSupportsWebAuthn,
	type PublicKeyCredentialCreationOptionsJSON,
	startRegistration,
	WebAuthnError,
} from "@simplewebauthn/browser";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { apiFetch, mutationErrorMessage } from "../api/fetcher";
import type {
	PasskeyInfo,
	PasskeyListResponse,
	WebAuthnCreationOptions,
} from "../api/generated/models";
import { useAuth } from "../auth/AuthContext";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";
import { FormItem, FormMessage } from "../components/ui/form";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { useI18n } from "../i18n/I18nContext";

/** Self-service passkey management (#1171): list registered WebAuthn
 * credentials, register a new one through a credential-gated begin/finish
 * ceremony, and delete individual keys. The server requires either a session
 * that already satisfied a second factor or the account password — the form
 * therefore only prompts for the password when the session lacks the mark. */
export function PasskeysCard() {
	const { t } = useI18n();
	const { session, refresh } = useAuth();
	const qc = useQueryClient();
	const [error, setError] = useState<string | null>(null);
	const [adding, setAdding] = useState(false);
	const [keyName, setKeyName] = useState("");
	const [password, setPassword] = useState("");
	const [confirmDelete, setConfirmDelete] = useState<PasskeyInfo | null>(null);
	const [deletePassword, setDeletePassword] = useState("");

	// The session never carries secondFactor for token/dev-anonymous callers —
	// those identities cannot reach this card anyway (self-service cookie gate).
	const needsPassword = session?.secondFactor !== true;
	const supported = browserSupportsWebAuthn();

	const passkeys = useQuery<PasskeyListResponse>({
		queryKey: ["passkeys"],
		queryFn: () => apiFetch("/api/v1/users/me/passkeys"),
	});
	const invalidate = () =>
		void qc.invalidateQueries({ queryKey: ["passkeys"] });

	// begin -> navigator.credentials.create -> finish. A wrong password fails
	// at begin before any authenticator prompt, so the dialog stays open.
	const add = useMutation({
		mutationFn: async () => {
			const options = await apiFetch<WebAuthnCreationOptions>(
				"/api/v1/users/me/passkeys/register/begin",
				{
					method: "POST",
					body: JSON.stringify({
						name: keyName,
						...(needsPassword ? { password } : {}),
					}),
				},
			);
			const credential = await startRegistration({
				optionsJSON:
					options.publicKey as unknown as PublicKeyCredentialCreationOptionsJSON,
			});
			return apiFetch<PasskeyInfo>(
				"/api/v1/users/me/passkeys/register/finish",
				{
					method: "POST",
					body: JSON.stringify({ name: keyName, credential }),
				},
			);
		},
		onSuccess: () => {
			setAdding(false);
			setKeyName("");
			setPassword("");
			setError(null);
			invalidate();
			// The server marks this session second-factor-persisted on
			// register/finish — refresh so needsPassword drops immediately
			// instead of waiting for a reload (#1171 review).
			void refresh();
		},
		onError: (err) => {
			// A dismissed authenticator prompt is not a failure worth an alarm.
			if (err instanceof WebAuthnError) {
				setError(
					err.code === "ERROR_CEREMONY_ABORTED"
						? t("settings.passkeys.cancelled")
						: t("settings.passkeys.error.register"),
				);
				return;
			}
			setError(
				mutationErrorMessage(err, t("settings.passkeys.error.register"), t),
			);
		},
	});

	const remove = useMutation({
		mutationFn: (id: string) =>
			apiFetch(`/api/v1/users/me/passkeys/${encodeURIComponent(id)}`, {
				method: "DELETE",
				body: JSON.stringify(needsPassword ? { password: deletePassword } : {}),
			}),
		onSuccess: () => {
			setConfirmDelete(null);
			setDeletePassword("");
			setError(null);
			invalidate();
		},
		onError: (err) =>
			setError(
				mutationErrorMessage(err, t("settings.passkeys.error.delete"), t),
			),
	});

	function onAddSubmit(e: FormEvent) {
		e.preventDefault();
		add.mutate();
	}

	function onDeleteSubmit(e: FormEvent) {
		e.preventDefault();
		if (confirmDelete) remove.mutate(confirmDelete.id);
	}

	const list = passkeys.data?.passkeys ?? [];

	return (
		<div className="card">
			<h2 style={{ fontSize: 15 }}>{t("settings.passkeys.title")}</h2>
			<p className="muted">{t("settings.passkeys.description")}</p>
			{error ? <FormMessage>{error}</FormMessage> : null}
			{!supported ? (
				<p className="muted">{t("settings.passkeys.unsupported")}</p>
			) : null}
			{passkeys.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : passkeys.isError ? (
				<FormMessage>{t("settings.passkeys.error.status")}</FormMessage>
			) : (
				<>
					{list.length === 0 ? (
						<p className="muted">{t("settings.passkeys.empty")}</p>
					) : (
						<div className="form-stack" style={{ marginBottom: 8 }}>
							{list.map((key) => (
								<div
									key={key.id}
									style={{
										display: "flex",
										gap: 12,
										alignItems: "center",
									}}
								>
									<div style={{ flex: 1, minWidth: 0 }}>
										<div>{key.name || key.id}</div>
										<div className="muted" style={{ fontSize: 12 }}>
											{key.createdAt
												? new Date(key.createdAt).toLocaleString()
												: "—"}
											{key.backedUp
												? ` · ${t("settings.passkeys.backedUp")}`
												: ""}
										</div>
									</div>
									{key.backedUp ? (
										<Badge variant="success">
											{t("settings.passkeys.synced")}
										</Badge>
									) : null}
									<Button
										size="sm"
										variant="danger"
										onClick={() => {
											setError(null);
											setDeletePassword("");
											setConfirmDelete(key);
										}}
									>
										{t("common.delete")}
									</Button>
								</div>
							))}
						</div>
					)}
					{supported ? (
						<Button
							variant="primary"
							disabled={add.isPending}
							onClick={() => {
								setError(null);
								setKeyName("");
								setPassword("");
								setAdding(true);
							}}
						>
							{t("settings.passkeys.add")}
						</Button>
					) : null}
				</>
			)}

			<Dialog open={adding} onOpenChange={setAdding}>
				<DialogContent>
					<DialogTitle>{t("settings.passkeys.addTitle")}</DialogTitle>
					<p className="muted">{t("settings.passkeys.addDescription")}</p>
					{error ? <FormMessage>{error}</FormMessage> : null}
					<form className="form-stack" onSubmit={onAddSubmit}>
						<FormItem>
							<Label htmlFor="passkey-name">
								{t("settings.passkeys.name")}
							</Label>
							<Input
								id="passkey-name"
								value={keyName}
								placeholder={t("settings.passkeys.namePlaceholder")}
								onChange={(e) => setKeyName(e.target.value)}
								required
							/>
						</FormItem>
						{needsPassword ? (
							<FormItem>
								<Label htmlFor="passkey-password">{t("auth.password")}</Label>
								<Input
									id="passkey-password"
									type="password"
									autoComplete="current-password"
									value={password}
									onChange={(e) => setPassword(e.target.value)}
									required
								/>
							</FormItem>
						) : null}
						<div style={{ display: "flex", gap: 8 }}>
							<Button
								type="submit"
								variant="primary"
								disabled={add.isPending || keyName.trim() === ""}
							>
								{add.isPending
									? t("settings.passkeys.registering")
									: t("settings.passkeys.add")}
							</Button>
							<Button type="button" onClick={() => setAdding(false)}>
								{t("common.cancel")}
							</Button>
						</div>
					</form>
				</DialogContent>
			</Dialog>

			<Dialog
				open={confirmDelete !== null}
				onOpenChange={(open) => {
					if (!open) setConfirmDelete(null);
				}}
			>
				<DialogContent>
					<DialogTitle>{t("settings.passkeys.deleteTitle")}</DialogTitle>
					<p className="muted">
						{t("settings.passkeys.deleteDescription", {
							name: confirmDelete?.name || confirmDelete?.id || "",
						})}
					</p>
					{error ? <FormMessage>{error}</FormMessage> : null}
					<form className="form-stack" onSubmit={onDeleteSubmit}>
						{needsPassword ? (
							<FormItem>
								<Label htmlFor="passkey-delete-password">
									{t("auth.password")}
								</Label>
								<Input
									id="passkey-delete-password"
									type="password"
									autoComplete="current-password"
									value={deletePassword}
									onChange={(e) => setDeletePassword(e.target.value)}
									required
								/>
							</FormItem>
						) : null}
						<div style={{ display: "flex", gap: 8 }}>
							<Button
								type="submit"
								variant="danger"
								disabled={
									remove.isPending ||
									(needsPassword && deletePassword.trim() === "")
								}
							>
								{remove.isPending
									? t("settings.passkeys.deleting")
									: t("common.delete")}
							</Button>
							<Button type="button" onClick={() => setConfirmDelete(null)}>
								{t("common.cancel")}
							</Button>
						</div>
					</form>
				</DialogContent>
			</Dialog>
		</div>
	);
}
