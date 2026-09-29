import { type FormEvent, useEffect, useRef, useState } from "react";
import { WebAuthnError } from "@simplewebauthn/browser";
import { ApiError } from "../api/fetcher";
import { useAuth } from "../auth/AuthContext";
import { type I18nVars, useI18n } from "../i18n/I18nContext";
import { takePendingLogin } from "../pendingLogin";

function loginFailureMessage(
	err: unknown,
	t: (key: string, vars?: I18nVars) => string,
): string {
	if (err instanceof ApiError) {
		if (err.status === 401 || err.status === 400) {
			return t("auth.login.invalid");
		}
		// 429 = username-keyed lockout (#667): name it and pass through the
		// Retry-After wait instead of the generic "try again" failure.
		if (err.status === 429) {
			const wait = err.retryAfterSeconds;
			return wait != null && wait > 0
				? t("auth.login.tooManyAttemptsWait", { seconds: wait })
				: t("auth.login.tooManyAttempts");
		}
	}
	return t("auth.login.failed");
}

function secondFactorFailureMessage(
	err: unknown,
	t: (key: string, vars?: I18nVars) => string,
): string {
	if (err instanceof ApiError) {
		if (err.status === 401 || err.status === 400) {
			return t("auth.totp.invalid");
		}
		if (err.status === 429) {
			const wait = err.retryAfterSeconds;
			return wait != null && wait > 0
				? t("auth.login.tooManyAttemptsWait", { seconds: wait })
				: t("auth.login.tooManyAttempts");
		}
	}
	return t("auth.totp.failed");
}

// Passkey failures (#1171) split three ways: a ceremony abort (user dismissed
// the prompt) is not an error worth alarming about, an ApiError mirrors the
// TOTP mapping, and anything else is a generic retryable failure.
function passkeyFailureMessage(
	err: unknown,
	t: (key: string, vars?: I18nVars) => string,
): string {
	if (err instanceof WebAuthnError) {
		if (err.code === "ERROR_CEREMONY_ABORTED") {
			return t("auth.passkey.cancelled");
		}
		return t("auth.passkey.failed");
	}
	if (err instanceof ApiError) {
		if (err.status === 401 || err.status === 400) {
			return t("auth.passkey.invalid");
		}
		if (err.status === 429) {
			const wait = err.retryAfterSeconds;
			return wait != null && wait > 0
				? t("auth.login.tooManyAttemptsWait", { seconds: wait })
				: t("auth.login.tooManyAttempts");
		}
	}
	return t("auth.passkey.failed");
}

export function LoginView() {
	const { login, verifySecondFactor, verifySecondFactorPasskey } = useAuth();
	const { t } = useI18n();
	const [pending] = useState(takePendingLogin);
	const [username, setUsername] = useState(pending.username);
	const [password, setPassword] = useState(pending.password);
	// Second stage of login (#1172): set when the password verified and the
	// account holds a second factor — the pending_2fa cookie was already
	// minted. `factorMethods` carries the server's advertised mechanisms
	// ("webauthn", "totp") so the view can offer a choice (#1171).
	const [factorStep, setFactorStep] = useState(false);
	const [factorMethods, setFactorMethods] = useState<string[]>([]);
	const [factorCode, setFactorCode] = useState("");
	const [pendingExpiresAt, setPendingExpiresAt] = useState<string | null>(null);
	const [recoveryMode, setRecoveryMode] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [busy, setBusy] = useState(false);
	const autoSubmitted = useRef(false);
	const tRef = useRef(t);
	tRef.current = t;

	async function submit(user: string, pass: string) {
		setError(null);
		setBusy(true);
		try {
			const result = await login(user, pass);
			if (result.secondFactorRequired) {
				setPendingExpiresAt(result.pendingExpiresAt ?? null);
				setFactorMethods(result.secondFactorMethods ?? []);
				setFactorStep(true);
			}
		} catch (err) {
			setError(loginFailureMessage(err, t));
		} finally {
			setBusy(false);
		}
	}

	async function onSubmit(e: FormEvent) {
		e.preventDefault();
		await submit(username, password);
	}

	async function onFactorSubmit(e: FormEvent) {
		e.preventDefault();
		setError(null);
		setBusy(true);
		try {
			await verifySecondFactor(
				recoveryMode ? { recoveryCode: factorCode } : { code: factorCode },
			);
		} catch (err) {
			setError(secondFactorFailureMessage(err, t));
		} finally {
			setBusy(false);
		}
	}

	async function onPasskey() {
		setError(null);
		setBusy(true);
		try {
			await verifySecondFactorPasskey();
		} catch (err) {
			setError(passkeyFailureMessage(err, t));
		} finally {
			setBusy(false);
		}
	}

	useEffect(() => {
		if (!pending.submit || !pending.username || !pending.password) return;
		if (autoSubmitted.current) return;
		autoSubmitted.current = true;
		setBusy(true);
		setError(null);
		void login(pending.username, pending.password)
			.then((result) => {
				if (result.secondFactorRequired) {
					setPendingExpiresAt(result.pendingExpiresAt ?? null);
					setFactorMethods(result.secondFactorMethods ?? []);
					setFactorStep(true);
				}
			})
			.catch((err) => {
				setError(loginFailureMessage(err, tRef.current));
			})
			.finally(() => {
				setBusy(false);
			});
	}, [pending, login]);

	if (factorStep) {
		// The server advertises the accepted mechanisms; when both exist the
		// user picks. A missing list keeps the TOTP form for backwards
		// compatibility with an older backend.
		const hasPasskey = factorMethods.includes("webauthn");
		const hasTotp = factorMethods.includes("totp") || !hasPasskey;
		return (
			<main className="center-screen">
				<div className="auth-card">
					<h1>Veil</h1>
					<div className="subtitle">{t("auth.secondFactor.subtitle")}</div>
					{hasPasskey ? (
						<button
							className="btn btn-primary"
							type="button"
							disabled={busy}
							onClick={() => void onPasskey()}
						>
							{busy ? t("auth.passkey.verifying") : t("auth.passkey.use")}
						</button>
					) : null}
					{hasPasskey && hasTotp ? (
						<div className="subtitle" style={{ margin: "8px 0" }}>
							{t("auth.secondFactor.or")}
						</div>
					) : null}
					{hasTotp ? (
						<form onSubmit={onFactorSubmit}>
							<div className="form-field">
								<label htmlFor="login-totp">
									{recoveryMode
										? t("auth.totp.recoveryCode")
										: t("auth.totp.code")}
								</label>
								<input
									id="login-totp"
									className="input"
									autoComplete={recoveryMode ? "off" : "one-time-code"}
									inputMode={recoveryMode ? "text" : "numeric"}
									value={factorCode}
									onChange={(e) => setFactorCode(e.target.value)}
									required
								/>
							</div>
							<button
								className={`btn${hasPasskey ? "" : " btn-primary"}`}
								type="submit"
								disabled={busy}
							>
								{busy ? t("auth.totp.verifying") : t("auth.totp.verify")}
							</button>
							<button
								className="btn"
								type="button"
								onClick={() => {
									setRecoveryMode((v) => !v);
									setFactorCode("");
									setError(null);
								}}
							>
								{recoveryMode
									? t("auth.totp.useAuthenticator")
									: t("auth.totp.useRecovery")}
							</button>
						</form>
					) : null}
					{error ? (
						<div className="form-error" role="alert">
							{error}
						</div>
					) : null}
					{pendingExpiresAt ? (
						<p className="muted">
							{t("auth.totp.challengeExpires", {
								time: new Date(pendingExpiresAt).toLocaleTimeString(),
							})}
						</p>
					) : null}
					<button
						className="btn"
						type="button"
						onClick={() => {
							// The pending_2fa challenge is single-shot server-side:
							// expired/exhausted cookies are dead. Going back lets a
							// fresh password submit mint a new challenge.
							setFactorStep(false);
							setFactorCode("");
							setRecoveryMode(false);
							setPendingExpiresAt(null);
							setError(null);
						}}
					>
						{t("auth.totp.backToPassword")}
					</button>
				</div>
			</main>
		);
	}

	return (
		<main className="center-screen">
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
		</main>
	);
}
