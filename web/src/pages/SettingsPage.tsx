import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch, ApiError } from "../api/fetcher";
import { useIsAdmin } from "../auth/AuthContext";
import type { Settings } from "../api/generated/models";
import { useState } from "react";

/** Settings: view + edit mutations for safe fields (email, panel access). */
export function SettingsPage() {
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [editing, setEditing] = useState(false);
	const [form, setForm] = useState({ email: "", panelAccess: "" });

	const settings = useQuery<Settings>({
		queryKey: ["settings"],
		queryFn: () => apiFetch("/api/settings"),
	});

	const save = useMutation({
		mutationFn: (patch: Partial<Settings>) =>
			apiFetch("/api/settings", { method: "PUT", body: JSON.stringify(patch) }),
		onSuccess: () => {
			setEditing(false);
			void qc.invalidateQueries({ queryKey: ["settings"] });
		},
	});

	const s = settings.data;
	const rows: Array<[string, string | undefined]> = [
		["Domain", s?.domain],
		["Mode", s?.mode],
		["Panel listen", s?.panelListen],
		["Panel access", s?.panelAccess],
		["Web base path", s?.webBasePath],
		["Email", s?.email],
	];

	function startEdit() {
		setForm({ email: s?.email ?? "", panelAccess: s?.panelAccess ?? "" });
		setEditing(true);
	}

	if (settings.isLoading) {
		return <div className="card"><p className="muted">Loading…</p></div>;
	}
	if (settings.isError || !settings.data) {
		return (
			<div className="card">
				<p className="form-error">
					{settings.error instanceof ApiError ? settings.error.message : "Settings unavailable"}
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
			</div>

			{editing ? (
				<div className="card">
					<h3>Edit settings</h3>
					<div style={{ display: "grid", gap: 12, maxWidth: 480 }}>
						<label>
							Email
							<input
								className="input"
								value={form.email}
								onChange={(e) => setForm({ ...form, email: e.target.value })}
							/>
						</label>
						<label>
							Panel access
							<select
								className="input"
								value={form.panelAccess}
								onChange={(e) => setForm({ ...form, panelAccess: e.target.value })}
							>
								<option value="">—</option>
								<option value="public">public</option>
								<option value="private">private</option>
							</select>
						</label>
						<div style={{ display: "flex", gap: 8 }}>
							<button
								type="button"
								className="btn btn-primary"
								disabled={save.isPending}
								onClick={() => {
								const patch: Record<string, string> = { email: form.email };
								if (form.panelAccess) patch.panelAccess = form.panelAccess;
								save.mutate(patch);
							}}
							>
								{save.isPending ? "Saving…" : "Save"}
							</button>
							<button type="button" className="btn" onClick={() => setEditing(false)}>
								Cancel
							</button>
						</div>
						{save.isError ? (
							<p className="form-error">
								{save.error instanceof ApiError ? save.error.message : "Save failed"}
							</p>
						) : null}
					</div>
				</div>
			) : null}

			<div className="card">
				<div className="table-container">
					<table className="data-table">
						<tbody>
							{rows.map(([k, v]) => (
								<tr key={k}>
									<td style={{ width: 200 }}><strong>{k}</strong></td>
									<td className="muted">{v || "—"}</td>
								</tr>
							))}
						</tbody>
					</table>
				</div>
				<p className="muted" style={{ fontSize: 12, marginTop: 12 }}>
					Server settings are applied through the apply pipeline. Use the CLI / setup flow to change
					listen address, domain, or base path safely.
				</p>
			</div>
		</>
	);
}
