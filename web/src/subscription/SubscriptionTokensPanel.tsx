import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import { useIsAdmin } from "../auth/AuthContext";
import { useI18n } from "../i18n/I18nContext";
import { RevealLink } from "./RevealLink";

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
	hasSecret?: boolean;
	url?: string;
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

function absoluteSubURL(url: string): string {
	if (url.startsWith("http://") || url.startsWith("https://")) {
		return url;
	}
	return `${location.origin}${url.startsWith("/") ? url : `/${url}`}`;
}

function TokenLink({
	url,
	copied,
	onCopy,
}: {
	url: string;
	copied: boolean;
	onCopy: (text: string) => void;
}) {
	const { t } = useI18n();
	const abs = absoluteSubURL(url);
	return (
		<RevealLink
			value={abs}
			copied={copied}
			onCopy={onCopy}
			showLabel={t("subTokens.showLink")}
			hideLabel={t("subTokens.hideLink")}
			copyLabel={t("subTokens.copyUrl")}
			copiedLabel={t("subTokens.copied")}
			urlLabel={t("subTokens.urlLabel")}
		/>
	);
}

/** Subscription tokens: URL and QR stay on each token after reload. */
export function SubscriptionTokensPanel({ clientId }: { clientId: string }) {
	const { t } = useI18n();
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [label, setLabel] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [copied, setCopied] = useState<string | null>(null);
	const [issued, setIssued] = useState<IssuedToken | null>(null);

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
			setLabel("");
			setError(null);
			setIssued(res);
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
			setError(null);
			setIssued(res);
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

	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			setCopied(text);
			setTimeout(() => setCopied(null), 1500);
		} catch {
			/* clipboard unavailable */
		}
	}

	const items = tokens.data?.items ?? [];

	return (
		<div className="card">
			<h2>{t("subTokens.title")}</h2>
			<p className="muted" style={{ fontSize: 13, marginTop: 0 }}>
				{t("subTokens.alwaysAvailable")}
			</p>

			{error ? <p className="form-error">{error}</p> : null}

			{issued?.url || issued?.plaintext ? (
				<div className="form-stack" data-testid="issued-subscription-token">
					<p className="muted" style={{ fontSize: 13, marginTop: 0 }}>
						{t("subTokens.oneTime")}
					</p>
					{issued.url ? (
						<TokenLink
							url={issued.url}
							copied={copied === absoluteSubURL(issued.url)}
							onCopy={(text) => void copy(text)}
						/>
					) : null}
				</div>
			) : null}

			{isAdmin ? (
				<div className="h-scroll" style={{ gap: 8, marginBottom: 16 }}>
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
			) : tokens.isError ? (
				<p className="form-error">
					{tokens.error instanceof ApiError
						? tokens.error.message
						: t("subTokens.error.load")}
				</p>
			) : items.length === 0 ? (
				<p className="muted">{t("subTokens.empty")}</p>
			) : (
				<div className="form-stack">
					{items.map((tok) => (
						<div
							key={tok.id}
							data-testid="subscription-token"
							style={{
								border: "1px solid var(--border)",
								padding: 12,
							}}
						>
							<div
								style={{
									display: "flex",
									gap: 12,
									alignItems: "center",
									flexWrap: "wrap",
								}}
							>
								<strong>{tok.label || tok.prefix}</strong>
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
								<span className="muted" style={{ fontSize: 12 }}>
									{t("common.created")}: {fmtTime(tok.createdAt)}
								</span>
								{isAdmin && !tok.revokedAt ? (
									<span style={{ marginLeft: "auto", whiteSpace: "nowrap" }}>
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
									</span>
								) : null}
							</div>
							{tok.revokedAt ? null : tok.url ? (
								<TokenLink
									url={tok.url}
									copied={copied === absoluteSubURL(tok.url)}
									onCopy={(text) => void copy(text)}
								/>
							) : (
								<p className="muted" style={{ fontSize: 13, marginTop: 12 }}>
									{t("subTokens.urlUnavailable")}
								</p>
							)}
						</div>
					))}
				</div>
			)}
		</div>
	);
}
