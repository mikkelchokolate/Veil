import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import type { ClientView, Inbound } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";

/** S4: mutation envelope feedback (revision/applyJob/success). */
interface MutationFeedback {
	revision?: { desired?: number; applied?: number; state?: string };
	applyJob?: { id?: string; status?: string };
	success?: boolean;
}

const PROTOCOLS = ["naiveproxy", "hysteria2", "olcrtc", "mieru"] as const;
const TRANSPORTS = ["tcp", "udp"] as const;

interface InboundForm {
	name: string;
	protocol: string;
	transport: string;
	port: string;
	enabled: boolean;
	masqueradeURL: string;
	fallbackRoot: string;
	olcrtcRoomID: string;
}

const EMPTY: InboundForm = {
	name: "",
	protocol: "hysteria2",
	transport: "udp",
	port: "",
	enabled: true,
	masqueradeURL: "",
	fallbackRoot: "",
	olcrtcRoomID: "",
};

export function InboundsPage() {
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [error, setError] = useState<string | null>(null);
	const [feedback, setFeedback] = useState<MutationFeedback | null>(null);
	const [editing, setEditing] = useState<string | null>(null); // name being edited
	const [creating, setCreating] = useState(false);
	const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
	const [form, setForm] = useState<InboundForm>(EMPTY);

	const inbounds = useQuery<Inbound[]>({
		queryKey: ["inbounds", "all"],
		queryFn: () => apiFetch("/api/inbounds"),
	});
	// Attached clients per inbound (normalized clients read model).
	const clients = useQuery<{ items?: ClientView[] } | ClientView[]>({
		queryKey: ["clients", "for-inbounds"],
		queryFn: () => apiFetch("/api/v1/clients?pageSize=500"),
	});
	const clientList: ClientView[] = Array.isArray(clients.data)
		? clients.data
		: (clients.data?.items ?? []);

	function attachedClients(inboundName: string): ClientView[] {
		return clientList.filter((c) => (c.inboundIds ?? []).includes(inboundName));
	}

	function invalidate() {
		void qc.invalidateQueries({ queryKey: ["inbounds"] });
		void qc.invalidateQueries({ queryKey: ["clients"] });
		void qc.invalidateQueries({ queryKey: ["apply"] });
	}
	function record(body: unknown) {
		const b = body as MutationFeedback | undefined;
		if (b && (b.revision || b.applyJob || typeof b.success === "boolean")) {
			setFeedback(b);
		}
	}

	function toBody(f: InboundForm, keepName?: string) {
		const port = f.port === "" ? undefined : Number.parseInt(f.port, 10);
		const body: Record<string, unknown> = {
			name: keepName ?? f.name,
			protocol: f.protocol,
			transport: f.transport,
			enabled: f.enabled,
		};
		if (port != null) body.port = port;
		if (f.masqueradeURL) body.masqueradeURL = f.masqueradeURL;
		if (f.fallbackRoot) body.fallbackRoot = f.fallbackRoot;
		if (f.olcrtcRoomID) body.olcrtcRoomID = f.olcrtcRoomID;
		return body;
	}

	const create = useMutation({
		mutationFn: async (f: InboundForm) =>
			apiFetch("/api/inbounds", {
				method: "POST",
				body: JSON.stringify(toBody(f)),
			}),
		onSuccess: (data) => {
			setCreating(false);
			setForm(EMPTY);
			setError(null);
			record(data);
			invalidate();
		},
		onError: (e) =>
			setError(e instanceof ApiError ? e.message : "Create failed"),
	});

	const update = useMutation({
		mutationFn: async (f: InboundForm & { original: string }) =>
			apiFetch(`/api/inbounds/${encodeURIComponent(f.original)}`, {
				method: "PUT",
				body: JSON.stringify(toBody(f, f.original)),
			}),
		onSuccess: (data) => {
			setEditing(null);
			setError(null);
			record(data);
			invalidate();
		},
		onError: (e) =>
			setError(e instanceof ApiError ? e.message : "Update failed"),
	});

	const remove = useMutation({
		mutationFn: async (name: string) =>
			apiFetch(`/api/inbounds/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: (data) => {
			setConfirmDelete(null);
			setError(null);
			record(data);
			invalidate();
		},
		onError: (e) =>
			setError(e instanceof ApiError ? e.message : "Delete failed"),
	});

	function startCreate() {
		setForm(EMPTY);
		setCreating(true);
		setEditing(null);
	}
	function startEdit(ib: Inbound) {
		setForm({
			name: ib.name,
			protocol: ib.protocol,
			transport: ib.transport ?? "tcp",
			port: ib.port != null ? String(ib.port) : "",
			enabled: ib.enabled ?? true,
			masqueradeURL: ib.masqueradeURL ?? "",
			fallbackRoot: ib.fallbackRoot ?? "",
			olcrtcRoomID: ib.olcrtcRoomID ?? "",
		});
		setEditing(ib.name);
		setCreating(false);
	}

	const formCard =
		creating || editing ? (
			<div className="card">
				<h2 style={{ fontSize: 15 }}>
					{creating ? "New inbound" : `Edit ${editing}`}
				</h2>
				<div
					style={{
						display: "grid",
						gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))",
						gap: 12,
						marginTop: 8,
					}}
				>
					<div className="form-field">
						<label htmlFor="ib-name">Name</label>
						<input
							id="ib-name"
							className="input"
							value={form.name}
							disabled={!!editing}
							onChange={(e) => setForm({ ...form, name: e.target.value })}
						/>
					</div>
					<div className="form-field">
						<label htmlFor="ib-proto">Protocol</label>
						<select
							id="ib-proto"
							className="input"
							value={form.protocol}
							onChange={(e) => setForm({ ...form, protocol: e.target.value })}
						>
							{PROTOCOLS.map((p) => (
								<option key={p} value={p}>
									{p}
								</option>
							))}
						</select>
					</div>
					<div className="form-field">
						<label htmlFor="ib-trans">Transport</label>
						<select
							id="ib-trans"
							className="input"
							value={form.transport}
							onChange={(e) => setForm({ ...form, transport: e.target.value })}
						>
							{TRANSPORTS.map((t) => (
								<option key={t} value={t}>
									{t}
								</option>
							))}
						</select>
					</div>
					<div className="form-field">
						<label htmlFor="ib-port">Port</label>
						<input
							id="ib-port"
							className="input"
							inputMode="numeric"
							value={form.port}
							onChange={(e) => setForm({ ...form, port: e.target.value })}
						/>
					</div>
					<div className="form-field">
						<label htmlFor="ib-masq">Masquerade URL</label>
						<input
							id="ib-masq"
							className="input"
							value={form.masqueradeURL}
							onChange={(e) =>
								setForm({ ...form, masqueradeURL: e.target.value })
							}
						/>
					</div>
					<div className="form-field">
						<label htmlFor="ib-fb">Fallback root</label>
						<input
							id="ib-fb"
							className="input"
							value={form.fallbackRoot}
							onChange={(e) =>
								setForm({ ...form, fallbackRoot: e.target.value })
							}
						/>
					</div>
					{form.protocol === "olcrtc" ? (
						<div className="form-field">
							<label htmlFor="ib-room">olcRTC room ID</label>
							<input
								id="ib-room"
								className="input"
								value={form.olcrtcRoomID}
								onChange={(e) =>
									setForm({ ...form, olcrtcRoomID: e.target.value })
								}
							/>
						</div>
					) : null}
				</div>
				<label
					style={{
						display: "flex",
						gap: 8,
						alignItems: "center",
						margin: "12px 0",
					}}
				>
					<input
						type="checkbox"
						checked={form.enabled}
						onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
					/>
					<span>Enabled</span>
				</label>
				<div style={{ display: "flex", gap: 8 }}>
					<button
						type="button"
						className="btn btn-primary"
						disabled={create.isPending || update.isPending}
						onClick={() => {
							if (creating) {
								create.mutate(form);
							} else if (editing) {
								update.mutate({ ...form, original: editing });
							}
						}}
					>
						{creating ? "Create" : "Save"}
					</button>
					<button
						type="button"
						className="btn"
						onClick={() => {
							setCreating(false);
							setEditing(null);
							setForm(EMPTY);
						}}
					>
						Cancel
					</button>
				</div>
			</div>
		) : null;

	return (
		<>
			<div className="card">
				<div style={{ display: "flex", alignItems: "center", gap: 12 }}>
					<h2 style={{ margin: 0, flex: 1 }}>Inbounds</h2>
					{isAdmin ? (
						<button
							type="button"
							className="btn btn-primary"
							onClick={startCreate}
						>
							New inbound
						</button>
					) : null}
				</div>
			</div>

			{error ? (
				<div className="card">
					<p className="form-error">{error}</p>
				</div>
			) : null}

			{feedback ? (
				<div className="card">
					<div
						style={{
							display: "flex",
							gap: 12,
							alignItems: "center",
							flexWrap: "wrap",
						}}
					>
						<span
							className={
								feedback.success === false
									? "badge badge-danger"
									: "badge badge-success"
							}
						>
							{feedback.success === false ? "apply failed" : "saved"}
						</span>
						{feedback.revision ? (
							<span className="muted" style={{ fontSize: 13 }}>
								desired rev {feedback.revision.desired ?? "—"} · applied{" "}
								{feedback.revision.applied ?? "—"} ·{" "}
								{feedback.revision.state ?? ""}
							</span>
						) : null}
						{feedback.applyJob?.id ? (
							<span className="muted mono" style={{ fontSize: 12 }}>
								job {feedback.applyJob.id} ({feedback.applyJob.status})
							</span>
						) : null}
						<button
							type="button"
							className="btn"
							style={{ marginLeft: "auto" }}
							onClick={() => setFeedback(null)}
						>
							Dismiss
						</button>
					</div>
				</div>
			) : null}

			{formCard}

			<div className="card">
				{inbounds.isLoading ? (
					<p className="muted">Loading…</p>
				) : inbounds.isError ? (
					<p className="form-error">
						{inbounds.error instanceof ApiError
							? inbounds.error.message
							: "Failed to load inbounds"}
					</p>
				) : (inbounds.data ?? []).length === 0 ? (
					<p className="muted">No inbounds configured.</p>
				) : (
					<div className="table-container">
						<table className="data-table">
							<thead>
								<tr>
									<th>Name</th>
									<th>Protocol</th>
									<th>Transport</th>
									<th>Port</th>
									<th>Status</th>
									<th>Attached clients</th>
									{isAdmin ? <th>Actions</th> : null}
								</tr>
							</thead>
							<tbody>
								{(inbounds.data ?? []).map((ib) => {
									const attached = attachedClients(ib.name);
									return (
										<tr key={ib.name}>
											<td>{ib.name}</td>
											<td className="muted">{ib.protocol}</td>
											<td className="muted">{ib.transport ?? "—"}</td>
											<td className="muted">{ib.port ?? "—"}</td>
											<td>
												<span
													className={`badge${ib.enabled ? " badge-success" : ""}`}
												>
													{ib.enabled ? "enabled" : "disabled"}
												</span>
											</td>
											<td>
												{attached.length === 0 ? (
													<span className="muted">—</span>
												) : (
													<span
														style={{
															display: "flex",
															gap: 4,
															flexWrap: "wrap",
														}}
													>
														{attached.map((c) => (
															<Link
																key={c.id}
																to="/clients/$clientId"
																params={{ clientId: c.id }}
																className="badge"
															>
																{c.name}
															</Link>
														))}
													</span>
												)}
											</td>
											{isAdmin ? (
												<td>
													<div
														style={{
															display: "flex",
															gap: 6,
															flexWrap: "wrap",
														}}
													>
														<button
															type="button"
															className="btn"
															onClick={() => startEdit(ib)}
														>
															Edit
														</button>
														<button
															type="button"
															className="btn"
															disabled={update.isPending}
															onClick={() =>
																update.mutate({
																	name: ib.name,
																	protocol: ib.protocol,
																	transport: ib.transport ?? "tcp",
																	port: ib.port != null ? String(ib.port) : "",
																	enabled: !ib.enabled,
																	masqueradeURL: ib.masqueradeURL ?? "",
																	fallbackRoot: ib.fallbackRoot ?? "",
																	olcrtcRoomID: ib.olcrtcRoomID ?? "",
																	original: ib.name,
																})
															}
														>
															{ib.enabled ? "Disable" : "Enable"}
														</button>
														{confirmDelete === ib.name ? (
															<>
																<button
																	type="button"
																	className="btn btn-danger"
																	disabled={remove.isPending}
																	onClick={() => remove.mutate(ib.name)}
																>
																	Confirm
																</button>
																<button
																	type="button"
																	className="btn"
																	onClick={() => setConfirmDelete(null)}
																>
																	Cancel
																</button>
															</>
														) : (
															<button
																type="button"
																className="btn btn-danger"
																onClick={() => setConfirmDelete(ib.name)}
															>
																Delete
															</button>
														)}
													</div>
												</td>
											) : null}
										</tr>
									);
								})}
							</tbody>
						</table>
					</div>
				)}
			</div>
		</>
	);
}
