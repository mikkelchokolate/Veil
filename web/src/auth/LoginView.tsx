import { type FormEvent, useState } from "react";
import { useAuth } from "../auth/AuthContext";
import { useI18n } from "../i18n/I18nContext";

export function LoginView() {
	const { login } = useAuth();
	const { t } = useI18n();
	const [username, setUsername] = useState("");
	const [password, setPassword] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [busy, setBusy] = useState(false);

	async function onSubmit(e: FormEvent) {
		e.preventDefault();
		setError(null);
		setBusy(true);
		try {
			await login(username, password);
		} catch {
			setError(t("auth.login.invalid"));
		} finally {
			setBusy(false);
		}
	}

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
