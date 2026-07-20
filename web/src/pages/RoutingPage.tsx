import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import type { RoutingRule } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";

/** Routing rules: list + create/edit/delete mutations. */
export function RoutingPage() {
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [editing, setEditing] = useState<RoutingRule | null>(null);
	const [creating, setCreating] = useState(false);
	const [form, setForm] = useState({
		name: "",
		match: "",
		outbound: "",
		enabled: true,
	});

	const rules = useQuery<RoutingRule[]>({
		queryKey: ["routing", "rules"],
		queryFn: () => apiFetch("/api/routing/rules"),
	});

	const save = useMutation({
		mutationFn: (rule: RoutingRule) => {
			if (creating) {
				return apiFetch("/api/routing/rules", {
					method: "POST",
					body: JSON.stringify(rule),
				});
			}
			return apiFetch(`/api/routing/rules/${encodeURIComponent(rule.name)}`, {
				method: "PUT",
				body: JSON.stringify(rule),
			});
		},
		onSuccess: () => {
			setEditing(null);
			setCreating(false);
			setForm({ name: "", match: "", outbound: "", enabled: true });
			void qc.invalidateQueries({ queryKey: ["routing"] });
		},
	});

	const del = useMutation({
		mutationFn: (name: string) =>
			apiFetch(`/api/routing/rules/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: () => void qc.invalidateQueries({ queryKey: ["routing"] }),
	});

	function startEdit(r: RoutingRule) {
		setEditing(r);
		setCreating(false);
		setForm({
			name: r.name,
			match: r.match,
			outbound: r.outbound,
			enabled: r.enabled,
		});
	}

	function startCreate() {
		setEditing(null);
		setCreating(true);
		setForm({ name: "", match: "", outbound: "", enabled: true });
	}

	function cancel() {
		setEditing(null);
		setCreating(false);
		setForm({ name: "", match: "", outbound: "", enabled: true });
	}

	return (
		<>
			<div className="card">
				<div style={{ display: "flex", gap: 12, alignItems: "center" }}>
					<h2 style={{ margin: 0, flex: 1 }}>Routing rules</h2>
					{isAdmin ? (
						<button
							type="button"
							className="btn btn-primary"
							onClick={startCreate}
						>
							New rule
						</button>
					) : null}
				</div>
			</div>

			{creating || editing ? (
				<div className="card">
					<h3>{creating ? "Create rule" : `Edit ${editing?.name}`}</h3>
					<div style={{ display: "grid", gap: 12, maxWidth: 480 }}>
						<label>
							Name
							<input
								className="input"
								value={form.name}
								onChange={(e) => setForm({ ...form, name: e.target.value })}
								disabled={!creating}
							/>
						</label>
						<label>
							Match (CIDR, domain, or geoip)
							<input
								className="input"
								value={form.match}
								onChange={(e) => setForm({ ...form, match: e.target.value })}
							/>
						</label>
						<label>
							Outbound
							<input
								className="input"
								value={form.outbound}
								onChange={(e) => setForm({ ...form, outbound: e.target.value })}
							/>
						</label>
						<label style={{ display: "flex", gap: 8, alignItems: "center" }}>
							<input
								type="checkbox"
								checked={form.enabled}
								onChange={(e) =>
									setForm({ ...form, enabled: e.target.checked })
								}
							/>
							Enabled
						</label>
						<div style={{ display: "flex", gap: 8 }}>
							<button
								type="button"
								className="btn btn-primary"
								disabled={
									save.isPending || !form.name || !form.match || !form.outbound
								}
								onClick={() =>
									save.mutate({
										name: form.name,
										match: form.match,
										outbound: form.outbound,
										enabled: form.enabled,
									})
								}
							>
								{save.isPending ? "Saving…" : "Save"}
							</button>
							<button type="button" className="btn" onClick={cancel}>
								Cancel
							</button>
						</div>
						{save.isError ? (
							<p className="form-error">
								{save.error instanceof ApiError
									? save.error.message
									: "Save failed"}
							</p>
						) : null}
					</div>
				</div>
			) : null}

			<div className="card">
				{rules.isLoading ? (
					<p className="muted">Loading…</p>
				) : rules.isError ? (
					<p className="form-error">
						{rules.error instanceof ApiError
							? rules.error.message
							: "Failed to load routing rules"}
					</p>
				) : (
					<div className="table-container">
						<table className="data-table">
							<thead>
								<tr>
									<th>Name</th>
									<th>Match</th>
									<th>Outbound</th>
									<th>Status</th>
									{isAdmin ? <th /> : null}
								</tr>
							</thead>
							<tbody>
								{(rules.data ?? []).length === 0 ? (
									<tr>
										<td colSpan={isAdmin ? 5 : 4} className="muted">
											No routing rules configured.
										</td>
									</tr>
								) : (
									(rules.data ?? []).map((r) => (
										<tr key={r.name}>
											<td>{r.name}</td>
											<td className="muted">{r.match}</td>
											<td className="muted">{r.outbound}</td>
											<td>
												<span
													className={`badge ${r.enabled ? "badge-success" : ""}`}
												>
													{r.enabled ? "enabled" : "disabled"}
												</span>
											</td>
											{isAdmin ? (
												<td style={{ display: "flex", gap: 4 }}>
													<button
														type="button"
														className="btn"
														onClick={() => startEdit(r)}
													>
														Edit
													</button>
													<button
														type="button"
														className="btn btn-danger"
														disabled={del.isPending}
														onClick={() => del.mutate(r.name)}
													>
														Delete
													</button>
												</td>
											) : null}
										</tr>
									))
								)}
							</tbody>
						</table>
					</div>
				)}
			</div>
		</>
	);
}
