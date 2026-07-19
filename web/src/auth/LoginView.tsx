import { type FormEvent, useState } from "react";
import { useAuth } from "../auth/AuthContext";

export function LoginView() {
	const { login } = useAuth();
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
			setError("Invalid username or password.");
		} finally {
			setBusy(false);
		}
	}

	return (
		<div className="center-screen">
			<form className="auth-card" onSubmit={onSubmit}>
				<h1>Veil</h1>
				<div className="subtitle">Sign in to the panel</div>
				<div className="form-field">
					<label htmlFor="login-username">Username</label>
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
					<label htmlFor="login-password">Password</label>
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
				{error ? <div className="form-error" role="alert">{error}</div> : null}
				<button className="btn btn-primary" type="submit" disabled={busy} style={{ width: "100%", marginTop: 8 }}>
					{busy ? "Signing in…" : "Sign in"}
				</button>
			</form>
		</div>
	);
}
