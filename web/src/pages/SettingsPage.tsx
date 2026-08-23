import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch, mutationErrorMessage } from "../api/fetcher";
import type { Settings } from "../api/generated/models";
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
import { Button } from "../components/ui/button";
import { FormItem, FormMessage } from "../components/ui/form";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Select } from "../components/ui/select";
import { Table, TableBody, TableCell, TableRow } from "../components/ui/table";
import { useI18n } from "../i18n/I18nContext";

/** S4: full settings edit (every editable field) + security key rotation.
 * Fields are grouped: identity/access, protocol credentials, ACME, firewall.
 * The apply pipeline applies the result; the envelope surfaces revision/job. */
export function SettingsPage() {
	const { t } = useI18n();
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [editing, setEditing] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [notice, setNotice] = useState<string | null>(null);
	const [confirmRotate, setConfirmRotate] = useState(false);
	const [form, setForm] = useState<Record<string, string>>({});

	const settings = useQuery<Settings>({
		queryKey: ["settings"],
		queryFn: () => apiFetch("/api/settings"),
	});
	const s = settings.data;

	const GLOBAL_FIELDS: Array<{
		key: string;
		label: string;
		placeholder?: string;
		type?: "text" | "checkbox" | "select";
		options?: Array<{ value: string; label: string }>;
	}> = [
		{ key: "domain", label: t("settings.field.domain") },
		{ key: "panelDomain", label: t("settings.field.panelDomain") },
		{
			key: "panelAccess",
			label: t("settings.field.panelAccess"),
			placeholder: "local | direct | caddy",
		},
		{ key: "webBasePath", label: t("settings.field.webBasePath") },
		{ key: "email", label: t("settings.field.email") },
		{ key: "panelEmail", label: t("settings.field.panelEmail") },
		{ key: "panelPublicPort", label: t("settings.field.panelPublicPort") },
		{ key: "defaultAcmeEmail", label: t("settings.field.defaultAcmeEmail") },
		{
			key: "acmeChallengeMode",
			label: t("settings.field.acmeChallengeMode"),
			placeholder: "http-01 | dns-01",
		},
		{
			key: "defaultInboundPublicPort",
			label: t("settings.field.defaultInboundPublicPort"),
			placeholder: "e.g. 443",
		},
		{
			key: "firewallManagement",
			label: t("settings.field.firewallManagement"),
			type: "select",
			options: [
				{ value: "", label: t("settings.option.firewallDefault") },
				{ value: "true", label: t("settings.option.enabled") },
				{ value: "false", label: t("settings.option.disabled") },
			],
		},
	];

	// Protocol-scoped credentials exposed at the settings level act purely as a
	// fallback for inbounds that do not set their own value. With per-inbound
	// domains and credentials the panel configures protocols inside each
	// inbound; these global defaults exist for backward compatibility and must
	// not be presented as "the protocol configuration".
	const PROTOCOL_DEFAULT_FIELDS: Array<{
		key: string;
		label: string;
		placeholder?: string;
		type?: "text" | "checkbox" | "select";
		options?: Array<{ value: string; label: string }>;
	}> = [
		{ key: "naiveUsername", label: t("settings.field.naiveUsername") },
		{ key: "naivePassword", label: t("settings.field.naivePassword") },
		{ key: "hysteria2Password", label: t("settings.field.hysteria2Password") },
		{
			key: "hysteria2Insecure",
			label: t("settings.field.hysteria2Insecure"),
			type: "checkbox",
		},
		{ key: "masqueradeURL", label: t("settings.field.masqueradeURL") },
		{ key: "fallbackRoot", label: t("settings.field.fallbackRoot") },
		{ key: "olcrtcRoomID", label: t("settings.field.olcrtcRoomID") },
		{
			key: "olcrtcAuth",
			label: t("settings.field.olcrtcAuth"),
			type: "select",
			options: [
				{ value: "jitsi", label: "jitsi" },
				{ value: "telemost", label: "telemost" },
				{ value: "wbstream", label: "wbstream" },
			],
		},
		{
			key: "olcrtcTransport",
			label: t("settings.field.olcrtcTransport"),
			type: "select",
			options: [
				{ value: "datachannel", label: "datachannel" },
				{ value: "vp8channel", label: "vp8channel" },
				{ value: "seichannel", label: "seichannel" },
				{ value: "videochannel", label: "videochannel" },
			],
		},
	];

	const save = useMutation({
		mutationFn: (patch: Record<string, unknown>) =>
			apiFetch("/api/settings", { method: "PUT", body: JSON.stringify(patch) }),
		onSuccess: () => {
			setEditing(false);
			setError(null);
			setNotice(t("settings.saved"));
			void qc.invalidateQueries({ queryKey: ["settings"] });
		},
		onError: (e) =>
			setError(mutationErrorMessage(e, t("settings.saveFailed"))),
	});

	const rotateKey = useMutation({
		mutationFn: () =>
			apiFetch("/api/admin/rotate-key", {
				method: "POST",
				body: JSON.stringify({}),
			}),
		onSuccess: () => {
			setConfirmRotate(false);
			setError(null);
			setNotice(t("settings.rotated"));
		},
		onError: (e) => {
			setError(mutationErrorMessage(e, t("settings.rotateFailed")));
		},
	});

	const ALL_FIELDS = [...GLOBAL_FIELDS, ...PROTOCOL_DEFAULT_FIELDS];

	function startEdit() {
		const next: Record<string, string> = {};
		const sRecord = s as Record<string, unknown> | undefined;
		const pf = (sRecord?.protocolFields ?? {}) as Record<string, unknown>;
		for (const f of ALL_FIELDS) {
			// Prefer the flat value, but fall back to the protocolFields copy
			// for states created before the SPA echoed both (legacy panel or
			// older revisions); otherwise checkboxes/bools render wrong.
			const raw = sRecord?.[f.key] ?? pf[f.key];
			next[f.key] = raw != null ? String(raw) : "";
		}
		setForm(next);
		setEditing(true);
		setNotice(null);
		setError(null);
	}

	function saveEdit() {
		// The settings PUT contract replaces the whole settings object and
		// requires panelListen/mode, so the SPA echoes the current GET payload
		// and overlays the edited fields on top. Password-typed fields that
		// arrive as "[REDACTED]" are preserved server-side by the settings
		// redaction policy.
		const base: Record<string, unknown> =
			(s as Record<string, unknown> | undefined) != null
				? { ...(s as unknown as Record<string, unknown>) }
				: {};
		// Snapshot BEFORE overlaying edits: the no-op guard below must compare
		// the form against the pristine echo, not against the mutated base.
		const original = { ...base };
		// Shallow-copy protocolFields so clearing fields below does not mutate
		// the react-query cached settings object.
		if (
			typeof base.protocolFields === "object" &&
			base.protocolFields != null
		) {
			base.protocolFields = {
				...(base.protocolFields as Record<string, unknown>),
			};
		} else {
			base.protocolFields = {};
		}
		// Schema-declared settings (protocolFields-backed): the flat "" value
		// means "not provided", so writes must go into protocolFields too.
		// The flat value of these keys must always mirror the protocolFields
		// copy, otherwise a stale flat zero/"" wins the server-side flat
		// precedence check and silently reverts the user's edit (or clearing).
		const pfKeys = new Set([
			"naiveUsername",
			"naivePassword",
			"hysteria2Password",
			"masqueradeURL",
			"fallbackRoot",
			"olcrtcAuth",
			"olcrtcTransport",
			"olcrtcRoomID",
			"hysteria2Insecure",
			"panelDomain",
			"panelEmail",
			"panelPublicPort",
		]);
		const pf = base.protocolFields as Record<string, unknown>;
		for (const f of ALL_FIELDS) {
			const cur = String(original[f.key] ?? "");
			if (form[f.key] === cur) {
				continue;
			}
			const cleared = form[f.key] === "";
			if (f.key === "defaultInboundPublicPort" || f.key === "panelPublicPort") {
				// Empty or invalid input clears the port back to 0 (unset).
				// Number(x) || x would turn "0" back into the string "0",
				// which the Go decoder rejects (int field) with a 400, so
				// parse with an explicit NaN check instead.
				const n = Number(form[f.key]);
				const port = cleared || Number.isNaN(n) ? 0 : n;
				base[f.key] = port;
				// panelPublicPort is a schema key: mirror into protocolFields
				// so a stale pf copy cannot win the flat precedence check.
				if (f.key === "panelPublicPort") {
					pf[f.key] = port;
				}
			} else if (f.key === "firewallManagement") {
				// Empty input restores the default (nil = enabled).
				base[f.key] = cleared ? null : form[f.key] === "true";
			} else if (f.key === "hysteria2Insecure") {
				const value = cleared ? false : form[f.key] === "true";
				base[f.key] = value;
				pf[f.key] = value;
			} else if (pfKeys.has(f.key)) {
				if (cleared) {
					pf[f.key] = "";
				} else {
					pf[f.key] = form[f.key];
				}
				base[f.key] = form[f.key];
			} else if (f.key === "webBasePath") {
				// The server always inherits the live webBasePath when the
				// field is empty (it is required for caddy Panel access), so
				// a "cleared" input would silently no-op. Keep the echoed
				// value instead of pretending the clear succeeded.
				if ((form[f.key] ?? "") !== "" && form[f.key] !== cur) {
					base[f.key] = form[f.key];
				}
			} else {
				// Plain string fields may be cleared: an empty input must
				// overwrite the echoed GET value instead of being skipped.
				base[f.key] = form[f.key];
			}
		}
		// No field changed: avoid a no-op PUT that would still trigger the
		// full apply pipeline (re-render, service restarts, firewall sync).
		// Compare against the pristine echo snapshot, not the mutated base.
		// The echo may carry schema values only in protocolFields (legacy
		// states), so compare the form value against flat ?? protocolFields.
		const originalPf = (original.protocolFields ?? {}) as Record<
			string,
			unknown
		>;
		let changed = false;
		for (const f of ALL_FIELDS) {
			const originalValue = String(original[f.key] ?? originalPf[f.key] ?? "");
			if (form[f.key] !== originalValue) {
				changed = true;
				break;
			}
		}
		if (!changed) {
			setEditing(false);
			return;
		}
		const webBaseOnly =
			(form.webBasePath ?? "") === "" &&
			(form.webBasePath ?? "") !==
				String(original.webBasePath ?? originalPf.webBasePath ?? "") &&
			ALL_FIELDS.every((f) => {
				if (f.key === "webBasePath") return true;
				const originalValue = String(
					original[f.key] ?? originalPf[f.key] ?? "",
				);
				return form[f.key] === originalValue;
			});
		if (webBaseOnly) {
			setEditing(false);
			setError(t("settings.webBasePathRequired"));
			return;
		}
		save.mutate(base);
	}

	const rows: Array<[string, string | undefined]> = [
		[t("settings.field.domain"), s?.domain],
		[t("settings.field.mode"), s?.mode],
		[t("settings.field.panelListen"), s?.panelListen],
		[t("settings.field.panelAccess"), s?.panelAccess],
		[
			t("settings.field.panelPublicPort"),
			(s as Record<string, unknown> | undefined)?.panelPublicPort != null
				? String((s as Record<string, unknown> | undefined)?.panelPublicPort)
				: undefined,
		],
		[
			t("settings.field.defaultInboundPublicPort"),
			(s as Record<string, unknown> | undefined)?.defaultInboundPublicPort !=
			null
				? String(
						(s as Record<string, unknown> | undefined)
							?.defaultInboundPublicPort,
					)
				: undefined,
		],
		[
			t("settings.field.firewallManagement"),
			(s as Record<string, unknown> | undefined)?.firewallManagement != null
				? String((s as Record<string, unknown> | undefined)?.firewallManagement)
				: undefined,
		],
		[t("settings.field.webBasePath"), s?.webBasePath],
		[t("settings.field.email"), s?.email],
		[
			t("settings.field.panelDomain"),
			(s as Record<string, unknown> | undefined)?.panelDomain as
				| string
				| undefined,
		],
		[t("settings.field.masqueradeURL"), s?.masqueradeURL],
		[t("settings.field.fallbackRoot"), s?.fallbackRoot],
		[
			t("settings.field.naiveUsername"),
			(s as Record<string, unknown> | undefined)?.naiveUsername as
				| string
				| undefined,
		],
		[
			t("settings.field.naivePassword"),
			(s as Record<string, unknown> | undefined)?.naivePassword as
				| string
				| undefined,
		],
		[
			t("settings.field.acmeEmail"),
			(s as Record<string, unknown> | undefined)?.defaultAcmeEmail as
				| string
				| undefined,
		],
		[
			t("settings.field.acmeChallenge"),
			(s as Record<string, unknown> | undefined)?.acmeChallengeMode as
				| string
				| undefined,
		],
	];

	if (settings.isLoading) {
		return (
			<div className="card">
				<p className="muted">{t("common.loading")}</p>
			</div>
		);
	}
	if (settings.isError || !settings.data) {
		return (
			<div className="card">
				<FormMessage>
					{settings.error instanceof ApiError
						? settings.error.message
						: t("settings.unavailable")}
				</FormMessage>
			</div>
		);
	}

	return (
		<>
			<div className="card">
				<div style={{ display: "flex", gap: 12, alignItems: "center" }}>
					<h2 style={{ margin: 0, flex: 1 }}>{t("settings.title")}</h2>
					{isAdmin && !editing ? (
						<Button onClick={startEdit}>{t("common.edit")}</Button>
					) : null}
				</div>
				{notice ? <p className="muted">{notice}</p> : null}
				{error ? <FormMessage>{error}</FormMessage> : null}
			</div>

			{editing ? (
				<div className="card">
					<h2 style={{ fontSize: 15 }}>{t("settings.editTitle")}</h2>
					<div className="form-stack">
						{GLOBAL_FIELDS.map((f) => (
							<FormItem key={f.key}>
								<Label htmlFor={`set-${f.key}`}>{f.label}</Label>
								{f.type === "select" ? (
									<Select
										id={`set-${f.key}`}
										value={form[f.key] ?? ""}
										onChange={(e) =>
											setForm({ ...form, [f.key]: e.target.value })
										}
									>
										{(f.options ?? []).map((o) => (
											<option key={o.value} value={o.value}>
												{o.label}
											</option>
										))}
									</Select>
								) : (
									<Input
										id={`set-${f.key}`}
										{...(f.placeholder ? { placeholder: f.placeholder } : {})}
										value={form[f.key] ?? ""}
										onChange={(e) =>
											setForm({ ...form, [f.key]: e.target.value })
										}
									/>
								)}
							</FormItem>
						))}
					</div>
					<h2
						style={{
							fontSize: 15,
							marginTop: 16,
							borderTop: "1px solid var(--border)",
							paddingTop: 12,
						}}
					>
						{t("settings.section.protocolDefaults")}
					</h2>
					<p className="muted" style={{ fontSize: 12 }}>
						{t("settings.section.protocolDefaultsHint")}
					</p>
					<div className="form-stack" style={{ marginTop: 8 }}>
						{PROTOCOL_DEFAULT_FIELDS.map((f) => (
							<FormItem key={f.key}>
								<Label htmlFor={`set-${f.key}`}>{f.label}</Label>
								{f.type === "checkbox" ? (
									<input
										id={`set-${f.key}`}
										type="checkbox"
										checked={form[f.key] === "true"}
										onChange={(e) =>
											setForm({
												...form,
												[f.key]: e.target.checked ? "true" : "false",
											})
										}
										style={{ width: 18, height: 18 }}
									/>
								) : f.type === "select" ? (
									<Select
										id={`set-${f.key}`}
										value={form[f.key] ?? ""}
										onChange={(e) =>
											setForm({ ...form, [f.key]: e.target.value })
										}
									>
										{(f.options ?? []).map((o) => (
											<option key={o.value} value={o.value}>
												{o.label}
											</option>
										))}
									</Select>
								) : (
									<Input
										id={`set-${f.key}`}
										{...(f.placeholder ? { placeholder: f.placeholder } : {})}
										value={form[f.key] ?? ""}
										onChange={(e) =>
											setForm({ ...form, [f.key]: e.target.value })
										}
									/>
								)}
							</FormItem>
						))}
					</div>
					<div style={{ display: "flex", gap: 8, marginTop: 12 }}>
						<Button
							variant="primary"
							disabled={save.isPending}
							onClick={saveEdit}
						>
							{save.isPending ? t("settings.saving") : t("common.save")}
						</Button>
						<Button onClick={() => setEditing(false)}>
							{t("common.cancel")}
						</Button>
					</div>
				</div>
			) : null}

			<div className="card">
				<Table>
					<TableBody>
						{rows.map(([k, v]) => (
							<TableRow key={k}>
								<TableCell style={{ width: 200 }}>
									<strong>{k}</strong>
								</TableCell>
								<TableCell className="muted">{v || "—"}</TableCell>
							</TableRow>
						))}
					</TableBody>
				</Table>
				<p className="muted" style={{ fontSize: 12, marginTop: 12 }}>
					{t("settings.readOnlyHint")}
				</p>
			</div>

			{isAdmin ? (
				<div className="card">
					<h2 style={{ fontSize: 15 }}>{t("settings.security")}</h2>
					<p className="muted">{t("settings.rotateDescription")}</p>
					<Button variant="danger" onClick={() => setConfirmRotate(true)}>
						{t("settings.rotate")}
					</Button>
					<AlertDialog open={confirmRotate} onOpenChange={setConfirmRotate}>
						<AlertDialogContent>
							<AlertDialogHeader>
								<AlertDialogTitle>
									{t("settings.rotateConfirmTitle")}
								</AlertDialogTitle>
								<AlertDialogDescription>
									{t("settings.rotateConfirmDescription")}
								</AlertDialogDescription>
							</AlertDialogHeader>
							<AlertDialogFooter>
								<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
								<AlertDialogAction
									disabled={rotateKey.isPending}
									onClick={(e) => {
										e.preventDefault();
										rotateKey.mutate();
									}}
								>
									{rotateKey.isPending
										? t("settings.rotating")
										: t("settings.confirmRotation")}
								</AlertDialogAction>
							</AlertDialogFooter>
						</AlertDialogContent>
					</AlertDialog>
				</div>
			) : null}
		</>
	);
}
