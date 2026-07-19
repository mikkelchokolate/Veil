import { useQuery } from "@tanstack/react-query";
import { apiFetch, ApiError } from "../api/fetcher";

interface Settings {
	domain?: string;
	mode?: string;
	panelListen?: string;
	panelAccess?: string;
	webBasePath?: string;
	email?: string;
}

/** Settings: read-only view of the current panel/server settings. Editing
 * server settings is a staged, apply-gated mutation; shown read-only here to
 * avoid unsafe partial updates. */
export function SettingsPage() {
	const settings = useQuery<Settings>({
		queryKey: ["settings"],
		queryFn: () => apiFetch("/api/settings"),
	});

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

	const s = settings.data;
	const rows: Array<[string, string | undefined]> = [
		["Domain", s.domain],
		["Mode", s.mode],
		["Panel listen", s.panelListen],
		["Panel access", s.panelAccess],
		["Web base path", s.webBasePath],
		["Email", s.email],
	];

	return (
		<div className="card">
			<h2>Settings</h2>
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
	);
}
