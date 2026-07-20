import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import type { Settings } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";

/** S4: full settings edit (every editable field) + security key rotation.
 * Fields are grouped: identity/access, protocol credentials, ACME, firewall.
 * The apply pipeline applies the result; the envelope surfaces revision/job. */
export function SettingsPage() {
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [editing, setEditing] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [notice, setNotice] = useState<string | null>(null);
	const [confirmRotate, setConfirmRotate] = useState(false);
	const [form, setForm] = useState<Record<string, string>>({});

	const settings = useQuery<Settings>({
		queryKey: ["settings"],
		queryFn: () => apiFetch("/api/settings"),
	});
	const s = settings.data;

	const save = useMutation({
		mutationFn: (patch: Record<string, unknown>) =>
			apiFetch("/api/settings", { method: "PUT", body: JSON.stringify(patch) }),
		onSuccess: () => {
			setEditing(false);
			setError(null);
			setNotice("Settings saved.");
			void qc.invalidateQueries({ queryKey: ["settings"] });
		},
		onError: (e) => setError(e instanceof ApiError ? e.message : "Save failed"),
	});

	const rotateKey = useMutation({
		mutationFn: () =>
			apiFetch("/api/admin/rotate-key", {
				method: "POST",
				body: JSON.stringify({}),
			}),
		onSuccess: () => {
			setConfirmRotate(false);
			setError(null);
			setNotice("State key rotated. Other sessions were revoked.");
		},
		onError: (e) =>
			setError(e instanceof ApiError ? e.message : "Key rotation failed"),
	});

	const FIELDS: Array<{ key: string; label: string; placeholder?: string }> = [
		{ key: "domain", label: "Domain" },
		{ key: "panelDomain", label: "Panel domain" },
		{
			key: "panelAccess",
			label: "Panel access",
			placeholder: "public | private",
		},
		{ key: "webBasePath", label: "Web base path" },
		{ key: "email", label: "Email" },
		{ key: "panelEmail", label: "Panel email" },
		{ key: "naiveUsername", label: "NaiveProxy username" },
		{ key: "hysteria2Password", label: "hysteria2 password" },
		{ key: "masqueradeURL", label: "Masquerade URL" },
		{ key: "fallbackRoot", label: "Fallback root" },
		{ key: "olcrtcRoomID", label: "olcRTC room ID" },
		{ key: "defaultAcmeEmail", label: "Default ACME email" },
		{
			key: "acmeChallengeMode",
			label: "ACME challenge mode",
			placeholder: "http-01 | dns-01",
		},
	];

	function startEdit() {
		const next: Record<string, string> = {};
		for (const f of FIELDS) {
			next[f.key] = String(
				(s as Record<string, unknown> | undefined)?.[f.key] ?? "",
			);
		}
		setForm(next);
		setEditing(true);
		setNotice(null);
		setError(null);
	}

	function saveEdit() {
		const patch: Record<string, unknown> = {};
		for (const f of FIELDS) {
			const cur = String(
				(s as Record<string, unknown> | undefined)?.[f.key] ?? "",
			);
			if (form[f.key] !== cur) {
				if (form[f.key] !== "") patch[f.key] = form[f.key];
			}
		}
		if (Object.keys(patch).length === 0) {
			setEditing(false);
			return;
		}
		save.mutate(patch);
	}

	const rows: Array<[string, string | undefined]> = [
		["Domain", s?.domain],
		["Mode", s?.mode],
		["Panel listen", s?.panelListen],
		["Panel access", s?.panelAccess],
		["Web base path", s?.webBasePath],
		["Email", s?.email],
		[
			"Panel domain",
			(s as Record<string, unknown> | undefined)?.panelDomain as
				| string
				| undefined,
		],
		["Masquerade URL", s?.masqueradeURL],
		["Fallback root", s?.fallbackRoot],
		[
			"ACME email",
			(s as Record<string, unknown> | undefined)?.defaultAcmeEmail as
				| string
				| undefined,
		],
		[
			"ACME challenge",
			(s as Record<string, unknown> | undefined)?.acmeChallengeMode as
				| string
				| undefined,
		],
	];

	if (settings.isLoading) {
		return (
			<div className="card">
				<p className="muted">Loading…</p>
			</div>
		);
	}
	if (settings.isError || !settings.data) {
		return (
			<div className="card">
				<p className="form-error">
					{settings.error instanceof ApiError
						? settings.error.message
						: "Settings unavailable"}
				</p>
			</div>
		);
	}

	return (
		<>
			<div className="card">
				<div style={{ display: "flex", gap: 12, alignItems: "center" }}>
					<h2 style={{ margin: 0, flex: 1 }}>Settings</h2>
					{isAdmin && !editing ? (
						<button type="button" className="btn" onClick={startEdit}>
							Edit
						</button>
					) : null}
				</div>
				{notice ? <p className="muted">{notice}</p> : null}
				{error ? <p className="form-error">{error}</p> : null}
			</div>

			{editing ? (
				<div className="card">
					<h2 style={{ fontSize: 15 }}>Edit settings</h2>
					<div
						style={{
							display: "grid",
							gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
							gap: 12,
						}}
					>
						{FIELDS.map((f) => (
							<div className="form-field" key={f.key}>
								<label htmlFor={`set-${f.key}`}>{f.label}</label>
								<input
									id={`set-${f.key}`}
									className="input"
									placeholder={f.placeholder}
									value={form[f.key] ?? ""}
									onChange={(e) =>
										setForm({ ...form, [f.key]: e.target.value })
									}
								/>
							</div>
						))}
					</div>
					<div style={{ display: "flex", gap: 8, marginTop: 12 }}>
						<button
							type="button"
							className="btn btn-primary"
							disabled={save.isPending}
							onClick={saveEdit}
						>
							{save.isPending ? "Saving…" : "Save"}
						</button>
						<button
							type="button"
							className="btn"
							onClick={() => setEditing(false)}
						>
							Cancel
						</button>
					</div>
				</div>
			) : null}

			<div className="card">
				<div className="table-container">
					<table className="data-table">
						<tbody>
							{rows.map(([k, v]) => (
								<tr key={k}>
									<td style={{ width: 200 }}>
										<strong>{k}</strong>
									</td>
									<td className="muted">{v || "—"}</td>
								</tr>
							))}
						</tbody>
					</table>
				</div>
				<p className="muted" style={{ fontSize: 12, marginTop: 12 }}>
					Listen address and mode are changed through the CLI / setup flow.
					Everything else above is editable.
				</p>
			</div>

			{isAdmin ? (
				<div className="card">
					<h2 style={{ fontSize: 15 }}>Security</h2>
					<p className="muted">
						Rotate the state encryption key. This revokes every other session
						and re-encrypts the state file.
					</p>
					{confirmRotate ? (
						<div style={{ display: "flex", gap: 8 }}>
							<button
								type="button"
								className="btn btn-danger"
								disabled={rotateKey.isPending}
								onClick={() => rotateKey.mutate()}
							>
								{rotateKey.isPending ? "Rotating…" : "Confirm rotation"}
							</button>
							<button
								type="button"
								className="btn"
								onClick={() => setConfirmRotate(false)}
							>
								Cancel
							</button>
						</div>
					) : (
						<button
							type="button"
							className="btn btn-danger"
							onClick={() => setConfirmRotate(true)}
						>
							Rotate state key
						</button>
					)}
				</div>
			) : null}
		</>
	);
}
