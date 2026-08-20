import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
// Generated from docs/openapi.yaml via Orval — do NOT hand-write DTOs for the
// client create contract (blocker W4).
import type {
	ClientCreateResponse,
	IssuedCredential,
} from "../api/generated/models";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "../components/ui/dialog";
import { FormDescription, FormItem, FormMessage } from "../components/ui/form";
import { Input, Textarea } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { useI18n } from "../i18n/I18nContext";
import { decimalWithinSafeInteger, parseQuotaDecimal } from "../lib/bytes";

interface InboundOption {
	name: string;
	protocol: string;
	enabled?: boolean;
}

interface BindingDraft {
	inboundId: string;
	credential: string;
}

/** How long the one-time credential dialog stays before auto-clearing. */
const ISSUED_CRED_TIMEOUT_MS = 5 * 60 * 1000;

/** B6/B7: create form with General / Limits / Access (bindings+credentials) /
 * Review steps. Server-generated credentials arrive in the create response
 * (issuedCredentials) and are shown once in a modal; they are held only in
 * component state — never in the Query cache, URL, or web storage — and are
 * cleared on close, navigation, unmount, and after a timeout. */
export function ClientNewPage() {
	const { t } = useI18n();
	const navigate = useNavigate();
	const qc = useQueryClient();

	const inbounds = useQuery<{ items?: InboundOption[] } | InboundOption[]>({
		queryKey: ["inbounds", "all"],
		queryFn: () => apiFetch("/api/inbounds"),
	});

	const [step, setStep] = useState(0);
	const [name, setName] = useState("");
	const [email, setEmail] = useState("");
	const [notes, setNotes] = useState("");
	const [quotaBytes, setQuotaBytes] = useState("");
	const [expiresAt, setExpiresAt] = useState("");
	const [bindings, setBindings] = useState<BindingDraft[]>([]);
	// One-time issued credentials. Held ONLY in local state, cleared eagerly.
	const [issuedCreds, setIssuedCreds] = useState<IssuedCredential[]>([]);
	const [error, setError] = useState<string | null>(null);

	const clearIssued = () => setIssuedCreds([]);

	// Clear issued credentials on unmount (navigation away) and after a
	// timeout, so the one-time secret never lingers in memory.
	useEffect(() => {
		if (issuedCreds.length === 0) return;
		const t = setTimeout(() => setIssuedCreds([]), ISSUED_CRED_TIMEOUT_MS);
		return () => {
			clearTimeout(t);
			setIssuedCreds([]);
		};
	}, [issuedCreds.length]);

	const inboundList: InboundOption[] = Array.isArray(inbounds.data)
		? inbounds.data
		: (inbounds.data?.items ?? []);

	// Issue 3: quotaBytes crosses the wire as a JSON number — reject anything
	// above Number.MAX_SAFE_INTEGER (compared as an exact decimal string) and
	// any non-whole-byte input before it can reach the API.
	const quotaError = !quotaBytes
		? null
		: !/^\d+$/.test(quotaBytes)
			? t("clientNew.quotaInvalid")
			: !decimalWithinSafeInteger(quotaBytes)
				? t("clientNew.quotaTooLarge")
				: null;

	const create = useMutation({
		mutationFn: async () => {
			if (
				quotaBytes &&
				(!/^\d+$/.test(quotaBytes) || !decimalWithinSafeInteger(quotaBytes))
			) {
				throw new ApiError(400, t("clientNew.quotaTooLarge"));
			}
			const body: Record<string, unknown> = {
				name,
				enabled: true,
			};
			if (email) body.email = email;
			if (notes) body.notes = notes;
			if (quotaBytes) body.quotaBytes = parseQuotaDecimal(quotaBytes);
			if (
				quotaBytes &&
				bindings.some((b) => {
					const ib = inboundList.find((item) => item.name === b.inboundId);
					return !ib || ib.protocol !== "hysteria2" || ib.enabled === false;
				})
			) {
				throw new ApiError(400, t("clientNew.quotaHy2Only"));
			}
			if (expiresAt)
				body.expiresAt = Math.floor(new Date(expiresAt).getTime() / 1000);
			if (bindings.length > 0) {
				body.bindings = bindings.map((b) => ({
					inboundId: b.inboundId,
					...(b.credential ? { credential: b.credential } : {}),
				}));
			}
			return apiFetch<ClientCreateResponse>("/api/v1/clients", {
				method: "POST",
				body: JSON.stringify(body),
			});
		},
		onSuccess: (resp) => {
			// S2: the backend generated any missing credentials inside the create
			// transaction and returned their plaintext exactly once here. Do NOT
			// call the rotate endpoint (and never with inboundId) — the plaintext
			// is already in the response. Surface it in the one-time modal.
			setIssuedCreds(resp.issuedCredentials ?? []);
			void qc.invalidateQueries({ queryKey: ["clients"] });
			void qc.invalidateQueries({ queryKey: ["apply"] });
			setStep(3);
		},
		onError: (err) => {
			setError(
				err instanceof ApiError ? err.message : t("clientNew.createError"),
			);
		},
	});

	function toggleBinding(inboundId: string) {
		setBindings((prev) =>
			prev.some((b) => b.inboundId === inboundId)
				? prev.filter((b) => b.inboundId !== inboundId)
				: [...prev, { inboundId, credential: "" }],
		);
	}

	function setCred(inboundId: string, credential: string) {
		setBindings((prev) =>
			prev.map((b) => (b.inboundId === inboundId ? { ...b, credential } : b)),
		);
	}

	const steps = [
		t("clientNew.stepGeneral"),
		t("clientNew.stepLimits"),
		t("clientNew.stepAccess"),
		t("clientNew.stepReview"),
	];

	return (
		<Dialog
			open
			onOpenChange={(open) => {
				if (!open) void navigate({ to: "/clients" });
			}}
		>
			<DialogContent className="creation-dialog creation-dialog-client">
				<div className="card" style={{ maxWidth: 640 }}>
					<DialogTitle>{t("clientNew.title")}</DialogTitle>
					<div className="muted" style={{ marginBottom: 20, fontSize: 13 }}>
						{steps.map((s, i) => (
							<span
								key={s}
								style={{
									fontWeight: i === step ? 700 : 400,
									color: i === step ? "var(--text-main)" : undefined,
								}}
							>
								{i > 0 ? " · " : ""}
								{s}
							</span>
						))}
					</div>

					{step === 0 ? (
						<div className="creation-dialog-fields">
							<FormItem>
								<Label htmlFor="nc-name">{t("common.name")}</Label>
								<Input
									id="nc-name"
									value={name}
									onChange={(e) => setName(e.target.value)}
									required
								/>
							</FormItem>
							<FormItem>
								<Label htmlFor="nc-email">{t("clientNew.emailLabel")}</Label>
								<Input
									id="nc-email"
									type="email"
									value={email}
									onChange={(e) => setEmail(e.target.value)}
								/>
							</FormItem>
							<FormItem>
								<Label htmlFor="nc-notes">{t("clientNew.notesLabel")}</Label>
								<Textarea
									id="nc-notes"
									value={notes}
									onChange={(e) => setNotes(e.target.value)}
								/>
							</FormItem>
						</div>
					) : null}

					{step === 1 ? (
						<div className="creation-dialog-fields">
							<FormItem>
								<Label htmlFor="nc-quota">{t("clientNew.quotaLabel")}</Label>
								<Input
									id="nc-quota"
									type="number"
									min="0"
									value={quotaBytes}
									onChange={(e) => setQuotaBytes(e.target.value)}
								/>
								<FormDescription>{t("clientNew.quotaHint")}</FormDescription>
								{quotaError ? <FormMessage>{quotaError}</FormMessage> : null}
							</FormItem>
							<FormItem>
								<Label htmlFor="nc-exp">{t("clientNew.expiryLabel")}</Label>
								<Input
									id="nc-exp"
									type="date"
									value={expiresAt}
									onChange={(e) => setExpiresAt(e.target.value)}
								/>
							</FormItem>
						</div>
					) : null}

					{step === 2 ? (
						<fieldset
							className="form-field"
							style={{ border: "none", padding: 0 }}
						>
							<legend>{t("clientNew.bindingsLegend")}</legend>
							{inboundList.length === 0 ? (
								<p className="muted">{t("clientNew.noInbounds")}</p>
							) : (
								inboundList.map((ib) => {
									const bound = bindings.find((b) => b.inboundId === ib.name);
									return (
										<div
											key={ib.name}
											style={{
												border: "1px solid var(--border)",
												borderRadius: 0,
												padding: 12,
												marginBottom: 8,
											}}
										>
											<Label
												style={{
													display: "flex",
													alignItems: "center",
													gap: 8,
													cursor: "pointer",
												}}
											>
												<input
													type="checkbox"
													checked={!!bound}
													onChange={() => toggleBinding(ib.name)}
												/>
												<span>{ib.name}</span>
												<span className="muted" style={{ fontSize: 12 }}>
													{ib.protocol}
												</span>
											</Label>
											{bound ? (
												<Input
													className="mono"
													style={{ marginTop: 8 }}
													placeholder={t("clientNew.credentialPlaceholder")}
													value={bound.credential}
													onChange={(e) => setCred(ib.name, e.target.value)}
												/>
											) : null}
										</div>
									);
								})
							)}
						</fieldset>
					) : null}

					{step === 3 ? (
						<div>
							{create.isSuccess ? (
								<>
									<Badge variant="success">
										{t("clientNew.clientCreated")}
									</Badge>
									<Dialog
										open={issuedCreds.length > 0}
										onOpenChange={(open) => {
											if (!open) clearIssued();
										}}
									>
										<DialogContent>
											<DialogHeader>
												<DialogTitle>
													{t("clientNew.oneTimeCredentialsTitle")}
												</DialogTitle>
												<DialogDescription>
													{t("clientNew.oneTimeCredentialsDescription")}
												</DialogDescription>
											</DialogHeader>
											{issuedCreds.map((c) => (
												<div key={c.bindingId} style={{ marginBottom: 8 }}>
													<div className="muted" style={{ fontSize: 12 }}>
														{c.inboundId} ({c.kind})
													</div>
													<code className="mono">{c.plaintext}</code>
												</div>
											))}
											<DialogFooter>
												<Button
													type="button"
													variant="primary"
													onClick={() => {
														clearIssued();
														void navigate({ to: "/clients" });
													}}
												>
													{t("common.done")}
												</Button>
											</DialogFooter>
										</DialogContent>
									</Dialog>
									{issuedCreds.length === 0 ? (
										<div style={{ display: "flex", gap: 8, marginTop: 16 }}>
											<Button
												type="button"
												variant="primary"
												onClick={() => {
													clearIssued();
													void navigate({ to: "/clients" });
												}}
											>
												{t("common.done")}
											</Button>
										</div>
									) : null}
								</>
							) : (
								<>
									<h2 style={{ fontSize: 14 }}>
										{t("clientNew.reviewHeading")}
									</h2>
									<p>
										<strong>{t("clientNew.reviewName")}:</strong> {name}
									</p>
									{email ? (
										<p>
											<strong>{t("clientNew.reviewEmail")}:</strong> {email}
										</p>
									) : null}
									{quotaBytes ? (
										<p>
											<strong>{t("clientNew.reviewQuota")}:</strong>{" "}
											{t("clientNew.reviewQuotaValue", { quotaBytes })}
										</p>
									) : null}
									{expiresAt ? (
										<p>
											<strong>{t("clientNew.reviewExpires")}:</strong>{" "}
											{expiresAt}
										</p>
									) : null}
									<p>
										<strong>{t("clientNew.reviewBindings")}:</strong>{" "}
										{bindings.length}
									</p>
								</>
							)}
						</div>
					) : null}

					{error ? (
						<FormMessage style={{ marginTop: 8 }}>{error}</FormMessage>
					) : null}

					{step < 3 || !create.isSuccess ? (
						// A <div>, not a <form>: with a form, clicking "Review" advanced the
						// step and the SAME cursor position turned into the submit button, so
						// the trailing mouse-up submitted the create — the user never saw the
						// review screen. Explicit onClick keeps each action deliberate.
						<div style={{ display: "flex", gap: 8, marginTop: 20 }}>
							<Button
								type="button"
								disabled={create.isPending}
								onClick={() => {
									if (step === 0) {
										void navigate({ to: "/clients" });
										return;
									}
									setStep((s) => Math.max(0, s - 1));
								}}
							>
								{t("common.back")}
							</Button>
							{step < 2 ? (
								<Button
									type="button"
									variant="primary"
									disabled={
										(step === 0 && !name) || (step === 1 && quotaError != null)
									}
									onClick={() => setStep((s) => s + 1)}
								>
									{t("common.next")}
								</Button>
							) : step === 2 ? (
								<Button
									type="button"
									variant="primary"
									onClick={() => setStep(3)}
								>
									{t("clientNew.reviewButton")}
								</Button>
							) : (
								<Button
									type="button"
									variant="primary"
									disabled={create.isPending}
									onClick={() => {
										setError(null);
										create.mutate();
									}}
								>
									{create.isPending
										? t("clientNew.creating")
										: t("clientNew.createClient")}
								</Button>
							)}
						</div>
					) : null}
				</div>
			</DialogContent>
		</Dialog>
	);
}
