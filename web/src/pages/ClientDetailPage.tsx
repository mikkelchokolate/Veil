import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { ApiError, apiFetch } from "../api/fetcher";
import {
	deleteApiV1ClientsId,
	deleteApiV1ClientsIdBindingsBindingId,
	patchApiV1ClientsIdBindingsBindingId,
	postApiV1ClientsIdBindings,
	postApiV1ClientsIdCredentialsBindingIdRotate,
	putApiV1ClientsId,
} from "../api/generated/clients/clients";
import type { BindingView, ClientView } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";
import { fmtBytes } from "../lib/bytes";
import { ClientTrafficPanel } from "../subscription/ClientTrafficPanel";
import { SubscriptionTokensPanel } from "../subscription/SubscriptionTokensPanel";

type ClientDetail = ClientView;
type Tab = "overview" | "access" | "subscription" | "traffic" | "audit";

/** S3: revision/apply feedback returned by every mutation envelope. */
interface MutationFeedback {
	revision?: { desired?: number; applied?: number; state?: string };
	applyJob?: { id?: string; status?: string; desiredRevision?: number };
	success?: boolean;
}

/** S3: client edit form — RHF + Zod. quotaBytes is kept as a decimal string in
 * the form and converted with integer parsing (never Number() on raw bytes)
 * to avoid float precision loss on large quotas. */
const editSchema = z.object({
	name: z.string().min(1, "name is required"),
	email: z.string().email("invalid email").or(z.literal("")),
	enabled: z.boolean(),
	quotaBytes: z
		.string()
		.refine(
			(v) => v === "" || /^\d+$/.test(v),
			"must be a whole number of bytes",
		),
	expiresAt: z.string(),
	notes: z.string(),
});
type EditValues = z.infer<typeof editSchema>;

interface InboundOption {
	name: string;
	protocol: string;
	enabled?: boolean;
}

interface AuditEntry {
	id?: string;
	action?: string;
	target?: string;
	actor?: string;
	success?: boolean;
	details?: string;
	at?: number;
}

export function ClientDetailPage() {
	const { clientId } = useParams({ strict: false }) as { clientId: string };
	const isAdmin = useIsAdmin();
	const navigate = useNavigate();
	const qc = useQueryClient();
	const [tab, setTab] = useState<Tab>("overview");
	const [revealed, setRevealed] = useState<Record<string, string>>({});
	const [error, setError] = useState<string | null>(null);
	const [feedback, setFeedback] = useState<MutationFeedback | null>(null);
	const [confirmDelete, setConfirmDelete] = useState(false);
	const [attachInbound, setAttachInbound] = useState("");

	// Clear one-time revealed credentials on unmount/navigation.
	useEffect(() => () => setRevealed({}), []);

	const client = useQuery<ClientDetail>({
		queryKey: ["clients", clientId],
		queryFn: () => apiFetch(`/api/v1/clients/${clientId}`),
	});

	const inbounds = useQuery<{ items?: InboundOption[] } | InboundOption[]>({
		queryKey: ["inbounds", "all"],
		queryFn: () => apiFetch("/api/inbounds"),
	});
	const inboundList: InboundOption[] = Array.isArray(inbounds.data)
		? inbounds.data
		: (inbounds.data?.items ?? []);

	const audit = useQuery<{ items?: AuditEntry[] } | AuditEntry[]>({
		queryKey: ["clients", clientId, "audit"],
		queryFn: () => apiFetch(`/api/v1/clients/${clientId}/audit`),
		enabled: tab === "audit",
	});
	const auditItems: AuditEntry[] = Array.isArray(audit.data)
		? audit.data
		: (audit.data?.items ?? []);

	function recordFeedback(body: unknown) {
		const b = body as MutationFeedback | undefined;
		if (b && (b.revision || b.applyJob || typeof b.success === "boolean")) {
			setFeedback(b);
		}
	}

	// S3: edit form (RHF + Zod), populated from the loaded client.
	const form = useForm<EditValues>({
		resolver: zodResolver(editSchema),
		defaultValues: {
			name: "",
			email: "",
			enabled: true,
			quotaBytes: "",
			expiresAt: "",
			notes: "",
		},
	});
	useEffect(() => {
		const c = client.data;
		if (!c) return;
		const notes = (c as { notes?: string }).notes ?? "";
		form.reset({
			name: c.name ?? "",
			email: c.email ?? "",
			enabled: c.enabled ?? true,
			// Bytes as a decimal string; never through Number() lossy input paths.
			quotaBytes: c.quotaBytes != null ? String(c.quotaBytes) : "",
			expiresAt: c.expiresAt
				? new Date(c.expiresAt * 1000).toISOString().slice(0, 10)
				: "",
			notes,
		});
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [client.data, form.reset]);

	const invalidate = () => {
		void qc.invalidateQueries({ queryKey: ["clients", clientId] });
		void qc.invalidateQueries({ queryKey: ["clients"] });
		void qc.invalidateQueries({ queryKey: ["apply"] });
	};

	const save = useMutation({
		mutationFn: async (v: EditValues) => {
			const c = client.data;
			if (!c) throw new Error("client not loaded");
			// Integer-parse the byte string; empty means clear/unlimited.
			const quota =
				v.quotaBytes === "" ? undefined : Number.parseInt(v.quotaBytes, 10);
			const expires = v.expiresAt
				? Math.floor(new Date(v.expiresAt).getTime() / 1000)
				: undefined;
			const res = await putApiV1ClientsId(clientId, {
				name: v.name,
				...(v.email ? { email: v.email } : {}),
				enabled: v.enabled,
				...(quota != null ? { quotaBytes: quota } : {}),
				...(expires != null ? { expiresAt: expires } : {}),
				...(v.notes ? { notes: v.notes } : {}),
				version: c.version ?? 0,
			});
			return res.data;
		},
		onSuccess: (data) => {
			setError(null);
			recordFeedback(data);
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Save failed"),
	});

	const enableToggle = useMutation({
		mutationFn: async () => {
			const c = client.data;
			if (!c) throw new Error("client not loaded");
			const res = await putApiV1ClientsId(clientId, {
				name: c.name,
				...(c.email ? { email: c.email } : {}),
				enabled: !c.enabled,
				...(c.quotaBytes != null ? { quotaBytes: c.quotaBytes } : {}),
				...(c.expiresAt != null ? { expiresAt: c.expiresAt } : {}),
				version: c.version ?? 0,
			});
			return res.data;
		},
		onSuccess: (data) => {
			setError(null);
			recordFeedback(data);
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Update failed"),
	});

	const remove = useMutation({
		mutationFn: async () => {
			const res = await deleteApiV1ClientsId(clientId);
			return res.data;
		},
		onSuccess: () => {
			setConfirmDelete(false);
			void qc.invalidateQueries({ queryKey: ["clients"] });
			void navigate({ to: "/clients" });
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Delete failed"),
	});

	const rotate = useMutation({
		mutationFn: async (bindingId: string) => {
			const res = await postApiV1ClientsIdCredentialsBindingIdRotate(
				clientId,
				bindingId,
				{},
			);
			// The backend envelope merges plaintext + revision/applyJob/success.
			return res.data as unknown as MutationFeedback & { plaintext?: string };
		},
		onSuccess: (res, bindingId) => {
			if (res.plaintext) {
				setRevealed((prev) => ({
					...prev,
					[bindingId]: res.plaintext as string,
				}));
			}
			setError(null);
			recordFeedback(res);
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Rotate failed"),
	});

	const toggleBinding = useMutation({
		mutationFn: async (b: BindingView) => {
			const res = await patchApiV1ClientsIdBindingsBindingId(clientId, b.id, {
				enabled: !b.enabled,
				version: b.version ?? 0,
			});
			return res.data;
		},
		onSuccess: (data) => {
			setError(null);
			recordFeedback(data);
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Update failed"),
	});

	const attach = useMutation({
		mutationFn: async (inboundId: string) => {
			const res = await postApiV1ClientsIdBindings(clientId, { inboundId });
			return res.data;
		},
		onSuccess: (data) => {
			setAttachInbound("");
			setError(null);
			recordFeedback(data);
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Attach failed"),
	});

	const detach = useMutation({
		mutationFn: async (bindingId: string) => {
			const res = await deleteApiV1ClientsIdBindingsBindingId(
				clientId,
				bindingId,
			);
			return res.data;
		},
		onSuccess: (data) => {
			setError(null);
			recordFeedback(data);
			invalidate();
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Detach failed"),
	});

	if (client.isLoading) {
		return (
			<div className="card">
				<p className="muted">Loading…</p>
			</div>
		);
	}
	if (client.isError || !client.data) {
		return (
			<div className="card">
				<p className="form-error">
					{client.error instanceof ApiError
						? client.error.message
						: "Failed to load client"}
				</p>
			</div>
		);
	}

	const c = client.data;
	const boundInboundIds = new Set((c.bindings ?? []).map((b) => b.inboundId));
	const attachable = inboundList.filter((ib) => !boundInboundIds.has(ib.name));
	const tabs: { id: Tab; label: string }[] = [
		{ id: "overview", label: "Overview" },
		{ id: "access", label: "Access" },
		{ id: "subscription", label: "Subscription" },
		{ id: "traffic", label: "Traffic" },
		{ id: "audit", label: "Audit" },
	];

	return (
		<>
			<div className="card">
				<div style={{ display: "flex", alignItems: "center", gap: 12 }}>
					<h2 style={{ margin: 0, flex: 1 }}>{c.name}</h2>
					{isAdmin ? (
						<>
							<button
								type="button"
								className="btn"
								disabled={enableToggle.isPending}
								onClick={() => enableToggle.mutate()}
							>
								{c.enabled ? "Disable client" : "Enable client"}
							</button>
							{confirmDelete ? (
								<>
									<button
										type="button"
										className="btn btn-danger"
										disabled={remove.isPending}
										onClick={() => remove.mutate()}
									>
										Confirm delete
									</button>
									<button
										type="button"
										className="btn"
										onClick={() => setConfirmDelete(false)}
									>
										Cancel
									</button>
								</>
							) : (
								<button
									type="button"
									className="btn btn-danger"
									onClick={() => setConfirmDelete(true)}
								>
									Delete
								</button>
							)}
						</>
					) : null}
				</div>
				<div style={{ display: "flex", gap: 8, marginTop: 12 }}>
					{tabs.map((t) => (
						<button
							key={t.id}
							type="button"
							className={`btn${tab === t.id ? " btn-primary" : ""}`}
							onClick={() => setTab(t.id)}
						>
							{t.label}
						</button>
					))}
				</div>
			</div>

			{error ? (
				<div className="card">
					<p className="form-error">{error}</p>
				</div>
			) : null}

			{/* S3: revision/apply feedback surfaced after each mutation */}
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

			{tab === "overview" ? (
				<div className="card">
					<h2>Overview</h2>
					<p>
						<strong>Status:</strong> {c.status}
					</p>
					<p>
						<strong>Quota:</strong>{" "}
						{c.quotaBytes != null ? fmtBytes(c.quotaBytes) : "unlimited"}
					</p>
					<p>
						<strong>Expires:</strong>{" "}
						{c.expiresAt
							? new Date(c.expiresAt * 1000).toLocaleString()
							: "never"}
					</p>

					{isAdmin ? (
						<form
							onSubmit={form.handleSubmit((v) => save.mutate(v))}
							style={{ marginTop: 16, maxWidth: 480 }}
						>
							<h2 style={{ fontSize: 14 }}>Edit</h2>
							<div className="form-field">
								<label htmlFor="cd-name">Name</label>
								<input
									id="cd-name"
									className="input"
									{...form.register("name")}
								/>
								{form.formState.errors.name ? (
									<span className="form-error">
										{form.formState.errors.name.message}
									</span>
								) : null}
							</div>
							<div className="form-field">
								<label htmlFor="cd-email">Email</label>
								<input
									id="cd-email"
									className="input"
									type="email"
									{...form.register("email")}
								/>
								{form.formState.errors.email ? (
									<span className="form-error">
										{form.formState.errors.email.message}
									</span>
								) : null}
							</div>
							<div className="form-field">
								<label htmlFor="cd-quota">
									Quota (bytes, blank = unlimited)
								</label>
								<input
									id="cd-quota"
									className="input"
									inputMode="numeric"
									{...form.register("quotaBytes")}
								/>
								{form.formState.errors.quotaBytes ? (
									<span className="form-error">
										{form.formState.errors.quotaBytes.message}
									</span>
								) : null}
							</div>
							<div className="form-field">
								<label htmlFor="cd-exp">Expiry date</label>
								<input
									id="cd-exp"
									className="input"
									type="date"
									{...form.register("expiresAt")}
								/>
							</div>
							<div className="form-field">
								<label htmlFor="cd-notes">Notes</label>
								<textarea
									id="cd-notes"
									className="input"
									{...form.register("notes")}
								/>
							</div>
							<label
								style={{
									display: "flex",
									gap: 8,
									alignItems: "center",
									marginBottom: 12,
								}}
							>
								<input type="checkbox" {...form.register("enabled")} />
								<span>Enabled</span>
							</label>
							<button
								type="submit"
								className="btn btn-primary"
								disabled={save.isPending || !form.formState.isDirty}
							>
								{save.isPending ? "Saving…" : "Save changes"}
							</button>
						</form>
					) : null}
				</div>
			) : null}

			{tab === "access" ? (
				<div className="card">
					<h2>Bindings & credentials</h2>
					{(c.bindings ?? []).length === 0 ? (
						<p className="muted">No bindings.</p>
					) : (
						(c.bindings ?? []).map((b) => (
							<div
								key={b.id}
								style={{
									border: "1px solid var(--border)",
									borderRadius: 6,
									padding: 12,
									marginBottom: 8,
								}}
							>
								<div
									style={{
										display: "flex",
										alignItems: "center",
										gap: 12,
										flexWrap: "wrap",
									}}
								>
									<strong>{b.inboundId}</strong>
									<span className={`badge${b.enabled ? " badge-success" : ""}`}>
										{b.enabled ? "enabled" : "disabled"}
									</span>
									{b.capability?.protocol ? (
										<span className="muted" style={{ fontSize: 12 }}>
											{b.capability.protocol}
										</span>
									) : null}
									<div style={{ flex: 1 }} />
									{isAdmin ? (
										<>
											<button
												type="button"
												className="btn"
												disabled={toggleBinding.isPending}
												onClick={() => toggleBinding.mutate(b)}
											>
												{b.enabled ? "Disable" : "Enable"}
											</button>
											<button
												type="button"
												className="btn"
												disabled={rotate.isPending}
												onClick={() => rotate.mutate(b.id)}
											>
												Rotate credential
											</button>
											<button
												type="button"
												className="btn btn-danger"
												disabled={detach.isPending}
												onClick={() => detach.mutate(b.id)}
											>
												Detach
											</button>
										</>
									) : null}
								</div>
								{b.credential ? (
									<div className="muted" style={{ fontSize: 12, marginTop: 6 }}>
										credential{" "}
										{b.credential.configured ? "configured" : "not set"}
										{b.credential.version != null
											? ` · v${b.credential.version}`
											: ""}
									</div>
								) : null}
								{revealed[b.id] ? (
									<div style={{ marginTop: 8 }}>
										<div className="muted" style={{ fontSize: 12 }}>
											New credential (copy now — shown once):
										</div>
										<code className="mono">{revealed[b.id]}</code>
									</div>
								) : null}
							</div>
						))
					)}

					{isAdmin && attachable.length > 0 ? (
						<div style={{ display: "flex", gap: 8, marginTop: 12 }}>
							<select
								className="input"
								style={{ maxWidth: 240 }}
								value={attachInbound}
								onChange={(e) => setAttachInbound(e.target.value)}
								aria-label="attach inbound"
							>
								<option value="">Attach inbound…</option>
								{attachable.map((ib) => (
									<option key={ib.name} value={ib.name}>
										{ib.name} ({ib.protocol})
									</option>
								))}
							</select>
							<button
								type="button"
								className="btn"
								disabled={!attachInbound || attach.isPending}
								onClick={() => attach.mutate(attachInbound)}
							>
								Attach
							</button>
						</div>
					) : null}
				</div>
			) : null}

			{tab === "subscription" ? (
				<SubscriptionTokensPanel clientId={clientId} />
			) : null}

			{tab === "traffic" ? <ClientTrafficPanel clientId={clientId} /> : null}

			{tab === "audit" ? (
				<div className="card">
					<h2>Audit</h2>
					{audit.isLoading ? (
						<p className="muted">Loading…</p>
					) : audit.isError ? (
						<p className="muted">Audit log unavailable for this client.</p>
					) : auditItems.length === 0 ? (
						<p className="muted">No audit entries.</p>
					) : (
						<div className="table-container">
							<table className="data-table">
								<thead>
									<tr>
										<th>When</th>
										<th>Actor</th>
										<th>Action</th>
										<th>Result</th>
										<th>Details</th>
									</tr>
								</thead>
								<tbody>
									{auditItems.map((a, i) => (
										<tr key={a.id ?? i}>
											<td className="muted">
												{a.at ? new Date(a.at * 1000).toLocaleString() : "—"}
											</td>
											<td>{a.actor ?? "—"}</td>
											<td>{a.action ?? "—"}</td>
											<td>
												<span
													className={
														a.success === false
															? "badge badge-danger"
															: "badge badge-success"
													}
												>
													{a.success === false ? "failed" : "ok"}
												</span>
											</td>
											<td className="muted">{a.details ?? ""}</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
					)}
				</div>
			) : null}
		</>
	);
}
