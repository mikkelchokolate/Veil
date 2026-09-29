import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { ApiError, apiFetch, mutationErrorMessage } from "../api/fetcher";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";
import { FormItem, FormMessage } from "../components/ui/form";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { useI18n } from "../i18n/I18nContext";
import { QR } from "../subscription/QR";

interface TotpStatus {
	enabled: boolean;
	pendingEnrollment: boolean;
	recoveryCodesRemaining: number;
}

interface TotpEnrollment {
	secret: string;
	otpauthUri: string;
	issuer: string;
}

/** Self-service TOTP second-factor management (#1172): enroll wizard with a
 * QR/secret pair, one-time recovery codes shown exactly once, and a
 * credential-gated disable. Available to every cookie session (admin AND
 * viewer) — this is an account property, not an admin panel. */
export function TotpCard() {
	const { t } = useI18n();
	const qc = useQueryClient();
	const [error, setError] = useState<string | null>(null);
	const [enrollment, setEnrollment] = useState<TotpEnrollment | null>(null);
	const [confirmCode, setConfirmCode] = useState("");
	const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
	const [disabling, setDisabling] = useState(false);
	const [disableCode, setDisableCode] = useState("");
	const [copied, setCopied] = useState(false);

	const status = useQuery<TotpStatus>({
		queryKey: ["totp-status"],
		queryFn: () => apiFetch("/api/v1/users/me/totp"),
	});
	const invalidate = () =>
		void qc.invalidateQueries({ queryKey: ["totp-status"] });

	const enroll = useMutation({
		mutationFn: () =>
			apiFetch<TotpEnrollment>("/api/v1/users/me/totp/enroll", {
				method: "POST",
			}),
		onSuccess: (data) => {
			setEnrollment(data);
			setConfirmCode("");
			setRecoveryCodes(null);
			setError(null);
			invalidate();
		},
		onError: (err) =>
			setError(mutationErrorMessage(err, t("settings.totp.error.enroll"), t)),
	});

	const confirm = useMutation({
		mutationFn: () =>
			apiFetch<{ enabled: boolean; recoveryCodes: string[] }>(
				"/api/v1/users/me/totp/confirm",
				{ method: "POST", body: JSON.stringify({ code: confirmCode }) },
			),
		onSuccess: (data) => {
			setEnrollment(null);
			setConfirmCode("");
			setRecoveryCodes(data.recoveryCodes ?? []);
			setError(null);
			invalidate();
		},
		onError: (err) =>
			setError(mutationErrorMessage(err, t("settings.totp.error.confirm"), t)),
	});

	const disable = useMutation({
		mutationFn: () =>
			apiFetch("/api/v1/users/me/totp", {
				method: "DELETE",
				body: JSON.stringify({ code: disableCode }),
			}),
		onSuccess: () => {
			setDisabling(false);
			setDisableCode("");
			setError(null);
			invalidate();
		},
		onError: (err) =>
			setError(mutationErrorMessage(err, t("settings.totp.error.disable"), t)),
	});

	function onConfirmSubmit(e: FormEvent) {
		e.preventDefault();
		confirm.mutate();
	}

	function onDisableSubmit(e: FormEvent) {
		e.preventDefault();
		disable.mutate();
	}

	const enabled = status.data?.enabled === true;

	return (
		<div className="card">
			<h2 style={{ fontSize: 15 }}>{t("settings.totp.title")}</h2>
			<p className="muted">{t("settings.totp.description")}</p>
			{error ? <FormMessage>{error}</FormMessage> : null}
			{status.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : status.isError ? (
				<FormMessage>
					{status.error instanceof ApiError
						? status.error.message
						: t("settings.totp.error.status")}
				</FormMessage>
			) : (
				<div style={{ display: "flex", gap: 12, alignItems: "center" }}>
					<Badge variant={enabled ? "success" : "default"}>
						{enabled ? t("common.enabled") : t("common.disabled")}
					</Badge>
					{status.data?.pendingEnrollment ? (
						<span className="muted">
							{t("settings.totp.pendingEnrollment")}
						</span>
					) : null}
					{enabled ? (
						<span className="muted">
							{t("settings.totp.recoveryRemaining", {
								n: status.data?.recoveryCodesRemaining ?? 0,
							})}
						</span>
					) : null}
					<span style={{ flex: 1 }} />
					{enabled ? (
						<Button
							variant="danger"
							onClick={() => {
								setError(null);
								setDisabling(true);
							}}
						>
							{t("common.disable")}
						</Button>
					) : (
						<Button
							variant="primary"
							disabled={enroll.isPending}
							onClick={() => {
								setError(null);
								setRecoveryCodes(null);
								enroll.mutate();
							}}
						>
							{status.data?.pendingEnrollment
								? t("settings.totp.restartEnrollment")
								: t("settings.totp.enable")}
						</Button>
					)}
				</div>
			)}

			{enrollment ? (
				<form
					className="form-stack"
					style={{ marginTop: 12 }}
					onSubmit={onConfirmSubmit}
				>
					<p className="muted">{t("settings.totp.enrollScan")}</p>
					<div
						style={{
							display: "flex",
							gap: 16,
							alignItems: "center",
							flexWrap: "wrap",
						}}
					>
						<QR value={enrollment.otpauthUri} size={160} />
						<div style={{ minWidth: 220, flex: 1 }}>
							<FormItem>
								<Label>{t("settings.totp.secret")}</Label>
								<Input readOnly value={enrollment.secret} />
							</FormItem>
							<FormItem>
								<Label>{t("settings.totp.uri")}</Label>
								<Input readOnly value={enrollment.otpauthUri} />
							</FormItem>
						</div>
					</div>
					<FormItem>
						<Label htmlFor="totp-confirm-code">
							{t("settings.totp.confirmCode")}
						</Label>
						<Input
							id="totp-confirm-code"
							inputMode="numeric"
							autoComplete="one-time-code"
							value={confirmCode}
							onChange={(e) => setConfirmCode(e.target.value)}
							required
						/>
					</FormItem>
					<div style={{ display: "flex", gap: 8 }}>
						<Button
							type="submit"
							variant="primary"
							disabled={confirm.isPending || confirmCode.trim() === ""}
						>
							{confirm.isPending
								? t("settings.totp.confirming")
								: t("common.confirm")}
						</Button>
						<Button
							type="button"
							onClick={() => {
								setEnrollment(null);
								setConfirmCode("");
							}}
						>
							{t("common.cancel")}
						</Button>
					</div>
				</form>
			) : null}

			{recoveryCodes ? (
				<div style={{ marginTop: 12 }}>
					<h2 style={{ fontSize: 15 }}>{t("settings.totp.recoveryTitle")}</h2>
					<p className="muted">{t("settings.totp.recoveryHint")}</p>
					<div
						style={{
							display: "grid",
							gridTemplateColumns: "repeat(auto-fill, minmax(110px, 1fr))",
							gap: 6,
							fontFamily: "monospace",
						}}
					>
						{recoveryCodes.map((code) => (
							<code key={code}>{code}</code>
						))}
					</div>
					<div style={{ display: "flex", gap: 8, marginTop: 8 }}>
						<Button
							type="button"
							onClick={() => {
								void navigator.clipboard
									?.writeText(recoveryCodes.join("\n"))
									.then(() => setCopied(true))
									.catch(() => undefined);
							}}
						>
							{copied ? t("settings.totp.copied") : t("settings.totp.copy")}
						</Button>
						<Button
							type="button"
							variant="primary"
							onClick={() => setRecoveryCodes(null)}
						>
							{t("common.done")}
						</Button>
					</div>
				</div>
			) : null}

			<Dialog open={disabling} onOpenChange={setDisabling}>
				<DialogContent>
					<DialogTitle>{t("settings.totp.disableTitle")}</DialogTitle>
					<p className="muted">{t("settings.totp.disableDescription")}</p>
					{error ? <FormMessage>{error}</FormMessage> : null}
					<form className="form-stack" onSubmit={onDisableSubmit}>
						<FormItem>
							<Label htmlFor="totp-disable-code">{t("auth.totp.code")}</Label>
							<Input
								id="totp-disable-code"
								inputMode="numeric"
								autoComplete="one-time-code"
								value={disableCode}
								onChange={(e) => setDisableCode(e.target.value)}
								required
							/>
						</FormItem>
						<div style={{ display: "flex", gap: 8 }}>
							<Button
								type="submit"
								variant="danger"
								disabled={disable.isPending || disableCode.trim() === ""}
							>
								{disable.isPending
									? t("settings.totp.disabling")
									: t("common.disable")}
							</Button>
							<Button type="button" onClick={() => setDisabling(false)}>
								{t("common.cancel")}
							</Button>
						</div>
					</form>
				</DialogContent>
			</Dialog>
		</div>
	);
}
