import { useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { apiFetch } from "../api/fetcher";

export function SetupView() {
	const qc = useQueryClient();
	const [username, setUsername] = useState("admin");
	const [password, setPassword] = useState("");
	const [confirm, setConfirm] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [busy, setBusy] = useState(false);

	async function onSubmit(e: FormEvent) {
		e.preventDefault();
		setError(null);
		if (password !== confirm) {
			setError("Passwords do not match.");
			return;
		}
		setBusy(true);
		try {
			await apiFetch("/api/setup/complete", {
				method: "POST",
				body: JSON.stringify({ username, password }),
			});
			await qc.invalidateQueries();
		} catch (err) {
			setError(err instanceof Error ? err.message : "Setup failed.");
		} finally {
			setBusy(false);
		}
	}

	return (
		<div className="center-screen">
			<form className="auth-card" onSubmit={onSubmit}>
				<h1>Welcome to Veil</h1>
				<div className="subtitle">Create the initial administrator account</div>
				<div className="form-field">
					<label htmlFor="setup-username">Username</label>
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
					<label htmlFor="setup-password">Password</label>
					<input
						id="setup-password"
						className="input"
						type="password"
						autoComplete="new-password"
						value={password}
						onChange={(e) => setPassword(e.target.value)}
						required
					/>
				</div>
				<div className="form-field">
					<label htmlFor="setup-confirm">Confirm password</label>
					<input
						id="setup-confirm"
						className="input"
						type="password"
						autoComplete="new-password"
						value={confirm}
						onChange={(e) => setConfirm(e.target.value)}
						required
					/>
				</div>
				{error ? <div className="form-error" role="alert">{error}</div> : null}
				<button className="btn btn-primary" type="submit" disabled={busy} style={{ width: "100%", marginTop: 8 }}>
					{busy ? "Creating…" : "Create administrator"}
				</button>
			</form>
		</div>
	);
}
