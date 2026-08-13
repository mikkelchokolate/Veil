import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
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
	}> = [
		{ key: "domain", label: t("settings.field.domain") },
		{ key: "panelDomain", label: t("settings.field.panelDomain") },
		{
			key: "panelAccess",
			label: t("settings.field.panelAccess"),
			placeholder: "public | private",
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
			placeholder: "true | false",
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
	}> = [
		{ key: "naiveUsername", label: t("settings.field.naiveUsername") },
		{ key: "naivePassword", label: t("settings.field.naivePassword") },
		{ key: "hysteria2Password", label: t("settings.field.hysteria2Password") },
		{ key: "masqueradeURL", label: t("settings.field.masqueradeURL") },
		{ key: "fallbackRoot", label: t("settings.field.fallbackRoot") },
		{ key: "olcrtcRoomID", label: t("settings.field.olcrtcRoomID") },
		{ key: "olcrtcAuth", label: t("settings.field.olcrtcAuth") },
		{ key: "olcrtcTransport", label: t("settings.field.olcrtcTransport") },
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
			setError(e instanceof ApiError ? e.message : t("settings.saveFailed")),
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
			setError(e instanceof ApiError ? e.message : t("settings.rotateFailed"));
		},
	});

	const ALL_FIELDS = [...GLOBAL_FIELDS, ...PROTOCOL_DEFAULT_FIELDS];

	function startEdit() {
		const next: Record<string, string> = {};
		for (const f of ALL_FIELDS) {
			next[f.key] = String(
				(s as Record<string, unknown> | undefined)?.[f.key] ?? "",
			);
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
		for (const f of ALL_FIELDS) {
			const cur = String(base[f.key] ?? "");
			if (form[f.key] !== cur) {
				if (
					f.key === "defaultInboundPublicPort" ||
					f.key === "panelPublicPort"
				) {
					if (form[f.key] !== "") {
						const n = Number(form[f.key]);
						base[f.key] = Number.isNaN(n) ? form[f.key] : n;
					}
				} else if (
					f.key === "firewallManagement" ||
					f.key === "hysteria2Insecure"
				) {
					if (form[f.key] !== "") {
						base[f.key] = form[f.key] === "true";
					}
				} else {
					// String fields may be cleared: an empty input must
					// overwrite the echoed GET value instead of being skipped.
					base[f.key] = form[f.key];
				}
			}
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
					<div
						style={{
							display: "grid",
							gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
							gap: 12,
						}}
					>
						{GLOBAL_FIELDS.map((f) => (
							<FormItem key={f.key}>
								<Label htmlFor={`set-${f.key}`}>{f.label}</Label>
								<Input
									id={`set-${f.key}`}
									{...(f.placeholder ? { placeholder: f.placeholder } : {})}
									value={form[f.key] ?? ""}
									onChange={(e) =>
										setForm({ ...form, [f.key]: e.target.value })
									}
								/>
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
					<div
						style={{
							display: "grid",
							gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
							gap: 12,
							marginTop: 8,
						}}
					>
						{PROTOCOL_DEFAULT_FIELDS.map((f) => (
							<FormItem key={f.key}>
								<Label htmlFor={`set-${f.key}`}>{f.label}</Label>
								<Input
									id={`set-${f.key}`}
									{...(f.placeholder ? { placeholder: f.placeholder } : {})}
									value={form[f.key] ?? ""}
									onChange={(e) =>
										setForm({ ...form, [f.key]: e.target.value })
									}
								/>
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
