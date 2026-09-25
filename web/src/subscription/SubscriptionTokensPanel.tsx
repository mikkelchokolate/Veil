import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { ApiError, apiFetch, mutationErrorMessage } from "../api/fetcher";
import { useIsAdmin } from "../auth/AuthContext";
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "../components/ui/alert-dialog";
import { FormItem } from "../components/ui/form";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { useI18n } from "../i18n/I18nContext";
import { dateInputToUnix } from "../lib/localDate";
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

function tokenExpired(tok: SubscriptionToken, nowUnix: number): boolean {
	return tok.expiresAt != null && tok.expiresAt <= nowUnix;
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
	const [nowUnix, setNowUnix] = useState(() => Math.floor(Date.now() / 1000));
	const [renewId, setRenewId] = useState<string | null>(null);
	const [renewExpiry, setRenewExpiry] = useState("");
	// #699/#714: revoke and active-token rotate are irreversible for
	// subscribers — both confirm before firing, like the expired-renew
	// dialog below.
	const [revokeTarget, setRevokeTarget] = useState<SubscriptionToken | null>(
		null,
	);
	const [rotateTarget, setRotateTarget] = useState<SubscriptionToken | null>(
		null,
	);

	useEffect(() => {
		const id = window.setInterval(
			() => setNowUnix(Math.floor(Date.now() / 1000)),
			10_000,
		);
		return () => window.clearInterval(id);
	}, []);

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
			setError(mutationErrorMessage(err, t("subTokens.error.create"), t)),
	});

	const rotate = useMutation({
		mutationFn: ({
			tokenId,
			expiresAt,
		}: {
			tokenId: string;
			expiresAt?: number;
		}) =>
			apiFetch<IssuedToken>(
				`/api/v1/clients/${clientId}/tokens/${tokenId}/rotate`,
				{
					method: "POST",
					...(expiresAt != null ? { body: JSON.stringify({ expiresAt }) } : {}),
				},
			),
		onSuccess: (res) => {
			setError(null);
			setIssued(res);
			setRenewId(null);
			setRenewExpiry("");
			setRotateTarget(null);
			void qc.invalidateQueries({ queryKey: ["clients", clientId, "tokens"] });
		},
		onError: (err) =>
			setError(mutationErrorMessage(err, t("subTokens.error.rotate"), t)),
	});

	const revoke = useMutation({
		mutationFn: (tokenId: string) =>
			apiFetch(`/api/v1/clients/${clientId}/tokens/${tokenId}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			setError(null);
			setRevokeTarget(null);
			void qc.invalidateQueries({ queryKey: ["clients", clientId, "tokens"] });
		},
		onError: (err) =>
			setError(mutationErrorMessage(err, t("subTokens.error.revoke"), t)),
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

	function startRotate(tok: SubscriptionToken) {
		if (tokenExpired(tok, nowUnix)) {
			setRenewId(tok.id);
			setRenewExpiry("");
			setError(null);
			return;
		}
		// #714: active rotate invalidates every subscriber URL — confirm first.
		setRotateTarget(tok);
		setError(null);
	}

	function confirmRenew() {
		if (!renewId) return;
		const expires = dateInputToUnix(renewExpiry);
		if (Number.isNaN(expires) || expires <= nowUnix) {
			setError(t("subTokens.expiryMustBeFuture"));
			return;
		}
		rotate.mutate({ tokenId: renewId, expiresAt: expires });
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
					{items.map((tok) => {
						const expired = tokenExpired(tok, nowUnix);
						return (
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
									) : expired ? (
										<span className="badge badge-danger">
											{t("subTokens.status.expired")}
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
									{tok.expiresAt ? (
										<span className="muted" style={{ fontSize: 12 }}>
											{t("subTokens.expires")}: {fmtTime(tok.expiresAt)}
										</span>
									) : null}
									{isAdmin && !tok.revokedAt ? (
										<span style={{ marginLeft: "auto", whiteSpace: "nowrap" }}>
											<button
												type="button"
												className="btn"
												disabled={rotate.isPending}
												onClick={() => startRotate(tok)}
											>
												{t("subTokens.rotate")}
											</button>{" "}
											<button
												type="button"
												className="btn btn-danger"
												disabled={revoke.isPending}
												onClick={() => {
													setRevokeTarget(tok);
													setError(null);
												}}
											>
												{t("subTokens.revoke")}
											</button>
										</span>
									) : null}
								</div>
								{/* #725: the backend may still return a stored URL for
								 * expired/disabled tokens, but token lookup requires
								 * enabled + not expired — those URLs do not
								 * authenticate, so never offer them as copyable. */}
								{tok.revokedAt ? null : expired ? (
									<p className="muted" style={{ fontSize: 13, marginTop: 12 }}>
										{t("subTokens.urlExpired")}
									</p>
								) : !tok.enabled ? (
									<p className="muted" style={{ fontSize: 13, marginTop: 12 }}>
										{t("subTokens.urlDisabled")}
									</p>
								) : tok.url ? (
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
						);
					})}
				</div>
			)}

			<AlertDialog
				open={renewId !== null}
				onOpenChange={(open) => {
					if (!open) {
						setRenewId(null);
						setRenewExpiry("");
					}
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>
							{t("subTokens.rotateExpiredTitle")}
						</AlertDialogTitle>
						<AlertDialogDescription>
							{t("subTokens.rotateExpiredDescription")}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<FormItem>
						<Label htmlFor="token-renew-expiry">
							{t("subTokens.newExpiry")}
						</Label>
						<Input
							id="token-renew-expiry"
							type="date"
							value={renewExpiry}
							onChange={(e) => setRenewExpiry(e.target.value)}
						/>
					</FormItem>
					{/* A failed renew keeps this dialog open — the error (incl. the
						local expiry validation) must be visible here, not only
						behind the overlay (#649). */}
					{error ? <p className="form-error">{error}</p> : null}
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={rotate.isPending}
							onClick={(e) => {
								e.preventDefault();
								confirmRenew();
							}}
						>
							{rotate.isPending
								? t("subTokens.rotate")
								: t("subTokens.confirmRotate")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>

			{/* #714: rotating a live token invalidates every subscriber URL —
				confirm naming the token before the POST. */}
			<AlertDialog
				open={rotateTarget !== null}
				onOpenChange={(open) => {
					if (!open) setRotateTarget(null);
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>
							{t("subTokens.rotateActiveTitle")}
						</AlertDialogTitle>
						<AlertDialogDescription>
							{t("subTokens.rotateActiveDescription", {
								name: rotateTarget?.label || rotateTarget?.prefix || "",
							})}
						</AlertDialogDescription>
					</AlertDialogHeader>
					{error ? <p className="form-error">{error}</p> : null}
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={rotate.isPending}
							onClick={(e) => {
								e.preventDefault();
								if (rotateTarget) {
									rotate.mutate({ tokenId: rotateTarget.id });
								}
							}}
						>
							{rotate.isPending
								? t("subTokens.rotate")
								: t("subTokens.confirmRotateActive")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>

			{/* #699: revoke permanently kills the subscription URL — same
				confirm gate as the other destructive Panel actions. */}
			<AlertDialog
				open={revokeTarget !== null}
				onOpenChange={(open) => {
					if (!open) setRevokeTarget(null);
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>{t("subTokens.revokeTitle")}</AlertDialogTitle>
						<AlertDialogDescription>
							{t("subTokens.revokeDescription", {
								name: revokeTarget?.label || revokeTarget?.prefix || "",
							})}
						</AlertDialogDescription>
					</AlertDialogHeader>
					{error ? <p className="form-error">{error}</p> : null}
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={revoke.isPending}
							onClick={(e) => {
								e.preventDefault();
								if (revokeTarget) {
									revoke.mutate(revokeTarget.id);
								}
							}}
						>
							{revoke.isPending
								? t("subTokens.revoking")
								: t("subTokens.confirmRevoke")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</div>
	);
}
