import { useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { apiFetch } from "../api/fetcher";
import { useI18n } from "../i18n/I18nContext";

export function SetupView() {
	const qc = useQueryClient();
	const { t } = useI18n();
	const [username, setUsername] = useState("admin");
	const [password, setPassword] = useState("");
	const [confirm, setConfirm] = useState("");
	const [backupAck, setBackupAck] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [busy, setBusy] = useState(false);

	async function onSubmit(e: FormEvent) {
		e.preventDefault();
		setError(null);
		if (password !== confirm) {
			setError(t("auth.setup.mismatch"));
			return;
		}
		if (!backupAck) {
			setError(t("auth.setup.backupRequired"));
			return;
		}
		setBusy(true);
		try {
			await apiFetch("/api/setup/complete", {
				method: "POST",
				body: JSON.stringify({
					username,
					password,
					backupAcknowledged: true,
				}),
			});
			await qc.invalidateQueries();
		} catch (err) {
			setError(err instanceof Error ? err.message : t("auth.setup.failed"));
		} finally {
			setBusy(false);
		}
	}

	return (
		<div className="center-screen">
			<form className="auth-card" onSubmit={onSubmit}>
				<h1>{t("auth.setup.title")}</h1>
				<div className="subtitle">{t("auth.setup.subtitle")}</div>
				<div className="form-field">
					<label htmlFor="setup-username">{t("auth.username")}</label>
					<input
						id="setup-username"
						className="input"
						autoComplete="username"
						value={username}
						onChange={(e) => setUsername(e.target.value)}
						required
					/>
				</div>
				<div className="form-field">
					<label htmlFor="setup-password">{t("auth.password")}</label>
					<input
						id="setup-password"
						className="input"
						type="password"
						autoComplete="new-password"
						minLength={12}
						value={password}
						onChange={(e) => setPassword(e.target.value)}
						required
					/>
				</div>
				<div className="form-field">
					<label htmlFor="setup-confirm">{t("auth.setup.confirm")}</label>
					<input
						id="setup-confirm"
						className="input"
						type="password"
						autoComplete="new-password"
						minLength={12}
						value={confirm}
						onChange={(e) => setConfirm(e.target.value)}
						required
					/>
				</div>
				<label
					htmlFor="setup-backup-ack"
					style={{
						display: "flex",
						gap: 8,
						alignItems: "flex-start",
						marginTop: 8,
					}}
				>
					<input
						id="setup-backup-ack"
						type="checkbox"
						checked={backupAck}
						onChange={(e) => setBackupAck(e.target.checked)}
						required
					/>
					<span>{t("auth.setup.backupAck")}</span>
				</label>
				{error ? (
					<div className="form-error" role="alert">
						{error}
					</div>
				) : null}
				<button className="btn btn-primary" type="submit" disabled={busy}>
					{busy ? t("auth.setup.creating") : t("auth.setup.create")}
				</button>
			</form>
		</div>
	);
}
