import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useState } from "react";
import { apiFetch, ApiError } from "../api/fetcher";
import { ClientTrafficPanel } from "../subscription/ClientTrafficPanel";
import { SubscriptionTokensPanel } from "../subscription/SubscriptionTokensPanel";

interface BindingCapability {
	protocol?: string;
	transports?: string[];
	perClientCredentials?: boolean;
	requiresCaddy?: boolean;
}

interface CredentialMeta {
	configured?: boolean;
	kind?: string;
	version?: number;
	rotatedAt?: number;
}

interface BindingView {
	id: string;
	inboundId: string;
	enabled: boolean;
	version?: number;
	capability?: BindingCapability;
	credential?: CredentialMeta;
}

interface ClientDetail {
	id: string;
	name: string;
	email?: string;
	enabled?: boolean;
	quotaBytes?: number;
	expiresAt?: number;
	status: string;
	inboundIds?: string[];
	bindings?: BindingView[];
	version?: number;
	notes?: string;
}

type Tab = "overview" | "access" | "subscription" | "traffic";

export function ClientDetailPage() {
	const { clientId } = useParams({ strict: false }) as { clientId: string };
	const qc = useQueryClient();
	const [tab, setTab] = useState<Tab>("overview");
	const [revealed, setRevealed] = useState<Record<string, string>>({});
	const [error, setError] = useState<string | null>(null);

	const client = useQuery<ClientDetail>({
		queryKey: ["clients", clientId],
		queryFn: () => apiFetch(`/api/v1/clients/${clientId}`),
	});

	const rotate = useMutation({
		mutationFn: async (bindingId: string) =>
			apiFetch<{ plaintext?: string }>(
				`/api/v1/clients/${clientId}/credentials/${encodeURIComponent(bindingId)}/rotate`,
				{ method: "POST", body: JSON.stringify({}) },
			),
		onSuccess: (res, bindingId) => {
			if (res.plaintext) {
				setRevealed((prev) => ({ ...prev, [bindingId]: res.plaintext as string }));
			}
			setError(null);
			void qc.invalidateQueries({ queryKey: ["clients", clientId] });
		},
		onError: (err) => setError(err instanceof ApiError ? err.message : "Rotate failed"),
	});

	const toggleBinding = useMutation({
		mutationFn: async (b: BindingView) =>
			apiFetch(`/api/v1/clients/${clientId}/bindings/${encodeURIComponent(b.id)}`, {
				method: "PATCH",
				body: JSON.stringify({ enabled: !b.enabled, version: b.version ?? 0 }),
			}),
		onSuccess: () => {
			setError(null);
			void qc.invalidateQueries({ queryKey: ["clients", clientId] });
			void qc.invalidateQueries({ queryKey: ["apply"] });
		},
		onError: (err) => setError(err instanceof ApiError ? err.message : "Update failed"),
	});

	if (client.isLoading) {
		return <div className="card"><p className="muted">Loading…</p></div>;
	}
	if (client.isError || !client.data) {
		return (
			<div className="card">
				<p className="form-error">
					{client.error instanceof ApiError ? client.error.message : "Failed to load client"}
				</p>
			</div>
		);
	}

	const c = client.data;
	const tabs: { id: Tab; label: string }[] = [
		{ id: "overview", label: "Overview" },
		{ id: "access", label: "Access" },
		{ id: "subscription", label: "Subscription" },
		{ id: "traffic", label: "Traffic" },
	];

	return (
		<>
			<div className="card">
				<h2>{c.name}</h2>
				<div style={{ display: "flex", gap: 8, marginTop: 8 }}>
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
				<div className="card"><p className="form-error">{error}</p></div>
			) : null}

			{tab === "overview" ? (
				<div className="card">
					<h2>Overview</h2>
					<p><strong>Status:</strong> {c.status}</p>
					{c.email ? <p><strong>Email:</strong> {c.email}</p> : null}
					<p><strong>Enabled:</strong> {c.enabled ? "yes" : "no"}</p>
					<p><strong>Quota:</strong> {c.quotaBytes != null ? `${c.quotaBytes} bytes` : "unlimited"}</p>
					<p><strong>Expires:</strong> {c.expiresAt ? new Date(c.expiresAt * 1000).toLocaleString() : "never"}</p>
					{c.notes ? <p><strong>Notes:</strong> {c.notes}</p> : null}
				</div>
			) : null}

			{tab === "access" ? (
				<div className="card">
					<h2>Bindings & credentials</h2>
					{(c.bindings ?? []).length === 0 ? (
						<p className="muted">No bindings.</p>
					) : (
						(c.bindings ?? []).map((b) => (
							<div key={b.id} style={{ border: "1px solid var(--border)", borderRadius: 6, padding: 12, marginBottom: 8 }}>
								<div style={{ display: "flex", alignItems: "center", gap: 12 }}>
									<strong>{b.inboundId}</strong>
									<span className={`badge${b.enabled ? " badge-success" : ""}`}>
										{b.enabled ? "enabled" : "disabled"}
									</span>
									{b.capability?.protocol ? (
										<span className="muted" style={{ fontSize: 12 }}>{b.capability.protocol}</span>
									) : null}
									<div style={{ flex: 1 }} />
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
								</div>
								{b.credential ? (
									<div className="muted" style={{ fontSize: 12, marginTop: 6 }}>
										credential {b.credential.configured ? "configured" : "not set"}
										{b.credential.version != null ? ` · v${b.credential.version}` : ""}
									</div>
								) : null}
								{revealed[b.id] ? (
									<div style={{ marginTop: 8 }}>
										<div className="muted" style={{ fontSize: 12 }}>New credential (copy now — shown once):</div>
										<code className="mono">{revealed[b.id]}</code>
									</div>
								) : null}
							</div>
						))
					)}
				</div>
			) : null}

			{tab === "subscription" ? <SubscriptionTokensPanel clientId={clientId} /> : null}

			{tab === "traffic" ? <ClientTrafficPanel clientId={clientId} /> : null}
		</>
	);
}
