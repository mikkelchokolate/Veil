import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import { useIsAdmin } from "../auth/AuthContext";
import { QR } from "./QR";

interface SubscriptionToken {
	id: string;
	prefix: string;
	label?: string;
	enabled: boolean;
	expiresAt?: number;
	createdAt: number;
	rotatedAt?: number;
	revokedAt?: number;
	lastUsedAt?: number;
}

interface IssuedToken {
	token: SubscriptionToken;
	plaintext: string;
	url?: string;
}

function fmtTime(ts?: number): string {
	if (!ts) return "—";
	return new Date(ts * 1000).toLocaleDateString();
}

/** B8: subscription token management for a client. List / create / rotate /
 * revoke. One-time plaintext + subscription URL + QR shown once at issue or
 * rotate. Never persisted. */
export function SubscriptionTokensPanel({ clientId }: { clientId: string }) {
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [label, setLabel] = useState("");
	const [issued, setIssued] = useState<IssuedToken | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [copied, setCopied] = useState(false);

	const tokens = useQuery<{ items: SubscriptionToken[] }>({
		queryKey: ["clients", clientId, "tokens"],
		queryFn: () => apiFetch(`/api/v1/clients/${clientId}/tokens`),
	});

	const create = useMutation({
		mutationFn: () =>
			apiFetch<IssuedToken>(`/api/v1/clients/${clientId}/tokens`, {
				method: "POST",
				body: JSON.stringify({ label }),
			}),
		onSuccess: (res) => {
			setIssued(res);
			setLabel("");
			setError(null);
			void qc.invalidateQueries({ queryKey: ["clients", clientId, "tokens"] });
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Create failed"),
	});

	const rotate = useMutation({
		mutationFn: (tokenId: string) =>
			apiFetch<IssuedToken>(
				`/api/v1/clients/${clientId}/tokens/${tokenId}/rotate`,
				{
					method: "POST",
				},
			),
		onSuccess: (res) => {
			setIssued(res);
			setError(null);
			void qc.invalidateQueries({ queryKey: ["clients", clientId, "tokens"] });
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Rotate failed"),
	});

	const revoke = useMutation({
		mutationFn: (tokenId: string) =>
			apiFetch(`/api/v1/clients/${clientId}/tokens/${tokenId}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			setError(null);
			void qc.invalidateQueries({ queryKey: ["clients", clientId, "tokens"] });
		},
		onError: (err) =>
			setError(err instanceof ApiError ? err.message : "Revoke failed"),
	});

	const subURL = issued?.url ?? (issued ? `/s/${issued.plaintext}` : null);

	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			setCopied(true);
			setTimeout(() => setCopied(false), 1500);
		} catch {
			/* clipboard unavailable */
		}
	}

	return (
		<div className="card">
			<h2>Subscription tokens</h2>

			{issued && subURL ? (
				<div className="card" style={{ borderColor: "var(--border-hover)" }}>
					<h2 style={{ fontSize: 14 }}>
						New subscription — copy now (shown once)
					</h2>
					<div
						style={{
							display: "flex",
							gap: 20,
							alignItems: "flex-start",
							flexWrap: "wrap",
						}}
					>
						<QR
							value={
								subURL.startsWith("http")
									? subURL
									: `${location.origin}${subURL}`
							}
						/>
						<div style={{ flex: 1, minWidth: 240 }}>
							<div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
								Subscription URL
							</div>
							<code
								className="mono"
								style={{ wordBreak: "break-all", fontSize: 12 }}
							>
								{subURL.startsWith("http")
									? subURL
									: `${location.origin}${subURL}`}
							</code>
							<div style={{ marginTop: 12, display: "flex", gap: 8 }}>
								<button
									type="button"
									className="btn"
									onClick={() =>
										void copy(
											subURL.startsWith("http")
												? subURL
												: `${location.origin}${subURL}`,
										)
									}
								>
									{copied ? "Copied" : "Copy URL"}
								</button>
								<button
									type="button"
									className="btn"
									onClick={() => setIssued(null)}
								>
									Dismiss
								</button>
							</div>
							<p className="muted" style={{ fontSize: 12, marginTop: 8 }}>
								The URL and QR are shown only now. Rotating a token invalidates
								the old URL.
							</p>
						</div>
					</div>
				</div>
			) : null}

			{error ? <p className="form-error">{error}</p> : null}

			{isAdmin ? (
				<div style={{ display: "flex", gap: 8, marginBottom: 16 }}>
					<input
						className="input"
						style={{ maxWidth: 240 }}
						placeholder="Label (e.g. phone, laptop)"
						value={label}
						onChange={(e) => setLabel(e.target.value)}
					/>
					<button
						type="button"
						className="btn btn-primary"
						disabled={create.isPending}
						onClick={() => create.mutate()}
					>
						{create.isPending ? "Creating…" : "Create token"}
					</button>
				</div>
			) : null}

			{tokens.isLoading ? (
				<p className="muted">Loading…</p>
			) : (
				<div className="table-container">
					<table className="data-table">
						<thead>
							<tr>
								<th>Label</th>
								<th>Prefix</th>
								<th>Status</th>
								<th>Created</th>
								<th>Last used</th>
								<th>Expires</th>
								{isAdmin ? <th /> : null}
							</tr>
						</thead>
						<tbody>
							{(tokens.data?.items ?? []).length === 0 ? (
								<tr>
									<td colSpan={isAdmin ? 7 : 6} className="muted">
										No subscription tokens.
									</td>
								</tr>
							) : (
								(tokens.data?.items ?? []).map((t) => (
									<tr key={t.id}>
										<td>{t.label || <span className="muted">—</span>}</td>
										<td className="mono muted">{t.prefix}…</td>
										<td>
											{t.revokedAt ? (
												<span className="badge badge-danger">revoked</span>
											) : t.enabled ? (
												<span className="badge badge-success">active</span>
											) : (
												<span className="badge">disabled</span>
											)}
										</td>
										<td className="muted">{fmtTime(t.createdAt)}</td>
										<td className="muted">{fmtTime(t.lastUsedAt)}</td>
										<td className="muted">{fmtTime(t.expiresAt)}</td>
										{isAdmin ? (
											<td style={{ whiteSpace: "nowrap" }}>
												{!t.revokedAt ? (
													<>
														<button
															type="button"
															className="btn"
															disabled={rotate.isPending}
															onClick={() => rotate.mutate(t.id)}
														>
															Rotate
														</button>{" "}
														<button
															type="button"
															className="btn btn-danger"
															disabled={revoke.isPending}
															onClick={() => revoke.mutate(t.id)}
														>
															Revoke
														</button>
													</>
												) : null}
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
	);
}
