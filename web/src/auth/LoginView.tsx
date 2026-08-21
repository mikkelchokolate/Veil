import { type FormEvent, useEffect, useState } from "react";
import { useAuth } from "../auth/AuthContext";
import { useI18n } from "../i18n/I18nContext";
import { takePendingLogin } from "../pendingLogin";

export function LoginView() {
	const { login } = useAuth();
	const { t } = useI18n();
	const [pending] = useState(takePendingLogin);
	const [username, setUsername] = useState(pending.username);
	const [password, setPassword] = useState(pending.password);
	const [error, setError] = useState<string | null>(null);
	const [busy, setBusy] = useState(false);

	async function submit(user: string, pass: string) {
		setError(null);
		setBusy(true);
		try {
			await login(user, pass);
		} catch {
			setError(t("auth.login.invalid"));
		} finally {
			setBusy(false);
		}
	}

	async function onSubmit(e: FormEvent) {
		e.preventDefault();
		await submit(username, password);
	}

	useEffect(() => {
		if (!pending.submit || !pending.username || !pending.password) return;
		let cancelled = false;
		setBusy(true);
		setError(null);
		void login(pending.username, pending.password)
			.catch(() => {
				if (!cancelled) setError(t("auth.login.invalid"));
			})
			.finally(() => {
				if (!cancelled) setBusy(false);
			});
		return () => {
			cancelled = true;
		};
	}, [pending, login, t]);

	return (
		<div className="center-screen">
			<form className="auth-card" onSubmit={onSubmit}>
				<h1>Veil</h1>
				<div className="subtitle">{t("auth.login.subtitle")}</div>
				<div className="form-field">
					<label htmlFor="login-username">{t("auth.username")}</label>
					<input
						id="login-username"
						className="input"
						autoComplete="username"
						value={username}
						onChange={(e) => setUsername(e.target.value)}
						required
					/>
				</div>
				<div className="form-field">
					<label htmlFor="login-password">{t("auth.password")}</label>
					<input
						id="login-password"
						className="input"
						type="password"
						autoComplete="current-password"
						value={password}
						onChange={(e) => setPassword(e.target.value)}
						required
					/>
				</div>
				{error ? (
					<div className="form-error" role="alert">
						{error}
					</div>
				) : null}
				<button className="btn btn-primary" type="submit" disabled={busy}>
					{busy ? t("auth.login.signingIn") : t("auth.login.signIn")}
				</button>
			</form>
		</div>
	);
}
