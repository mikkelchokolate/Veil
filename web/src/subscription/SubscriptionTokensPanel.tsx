import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import { useIsAdmin } from "../auth/AuthContext";
import { useI18n } from "../i18n/I18nContext";
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
	const { t } = useI18n();
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
			setError(
				err instanceof ApiError ? err.message : t("subTokens.error.create"),
			),
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
			setError(
				err instanceof ApiError ? err.message : t("subTokens.error.rotate"),
			),
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
			setError(
				err instanceof ApiError ? err.message : t("subTokens.error.revoke"),
			),
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
			<h2>{t("subTokens.title")}</h2>

			{issued && subURL ? (
				<div className="card" style={{ borderColor: "var(--border-hover)" }}>
					<h2 style={{ fontSize: 14 }}>{t("subTokens.issuedTitle")}</h2>
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
								{t("subTokens.urlLabel")}
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
									{copied ? t("subTokens.copied") : t("subTokens.copyUrl")}
								</button>
								<button
									type="button"
									className="btn"
									onClick={() => setIssued(null)}
								>
									{t("subTokens.dismiss")}
								</button>
							</div>
							<p className="muted" style={{ fontSize: 12, marginTop: 8 }}>
								{t("subTokens.issuedNote")}
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
						placeholder={t("subTokens.labelPlaceholder")}
						value={label}
						onChange={(e) => setLabel(e.target.value)}
					/>
					<button
						type="button"
						className="btn btn-primary"
						disabled={create.isPending}
						onClick={() => create.mutate()}
					>
						{create.isPending ? t("subTokens.creating") : t("subTokens.create")}
					</button>
				</div>
			) : null}

			{tokens.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : (
				<div className="table-container">
					<table className="data-table">
						<thead>
							<tr>
								<th>{t("subTokens.label")}</th>
								<th>{t("subTokens.prefix")}</th>
								<th>{t("common.status")}</th>
								<th>{t("common.created")}</th>
								<th>{t("subTokens.lastUsed")}</th>
								<th>{t("subTokens.expires")}</th>
								{isAdmin ? <th /> : null}
							</tr>
						</thead>
						<tbody>
							{(tokens.data?.items ?? []).length === 0 ? (
								<tr>
									<td colSpan={isAdmin ? 7 : 6} className="muted">
										{t("subTokens.empty")}
									</td>
								</tr>
							) : (
								(tokens.data?.items ?? []).map((tok) => (
									<tr key={tok.id}>
										<td>{tok.label || <span className="muted">—</span>}</td>
										<td className="mono muted">{tok.prefix}…</td>
										<td>
											{tok.revokedAt ? (
												<span className="badge badge-danger">
													{t("subTokens.status.revoked")}
												</span>
											) : tok.enabled ? (
												<span className="badge badge-success">
													{t("subTokens.status.active")}
												</span>
											) : (
												<span className="badge">
													{t("subTokens.status.disabled")}
												</span>
											)}
										</td>
										<td className="muted">{fmtTime(tok.createdAt)}</td>
										<td className="muted">{fmtTime(tok.lastUsedAt)}</td>
										<td className="muted">{fmtTime(tok.expiresAt)}</td>
										{isAdmin ? (
											<td style={{ whiteSpace: "nowrap" }}>
												{!tok.revokedAt ? (
													<>
														<button
															type="button"
															className="btn"
															disabled={rotate.isPending}
															onClick={() => rotate.mutate(tok.id)}
														>
															{t("subTokens.rotate")}
														</button>{" "}
														<button
															type="button"
															className="btn btn-danger"
															disabled={revoke.isPending}
															onClick={() => revoke.mutate(tok.id)}
														>
															{t("subTokens.revoke")}
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
