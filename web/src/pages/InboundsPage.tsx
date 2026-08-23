import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { ApiError, type ApiValidationIssue, apiFetch } from "../api/fetcher";
import type { ClientView, Inbound } from "../api/generated/models";
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
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";
import { FormItem, FormMessage } from "../components/ui/form";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Select } from "../components/ui/select";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "../components/ui/table";
import { useI18n } from "../i18n/I18nContext";

/** S4: mutation envelope feedback (revision/applyJob/success). */
interface MutationFeedback {
	revision?: { desired?: number; applied?: number; state?: string };
	applyJob?: { id?: string; status?: string };
	success?: boolean;
}

type ProtocolField = {
	key: string;
	label: string;
	type: string;
	required?: boolean;
	default?: unknown;
	options?: Array<{ label: string; value: string }>;
	generateAction?: string;
	generateActionField?: string;
};

interface InboundForm {
	name: string;
	protocol: string;
	transport: string;
	port: string;
	enabled: boolean;
	masqueradeURL: string;
	fallbackRoot: string;
	olcrtcRoomID: string;
	password: string;
	profiles: NonNullable<Inbound["profiles"]>;
	naiveUsername: string;
	naivePassword: string;
	hysteria2Password: string;
	hysteria2Insecure: boolean;
	olcrtcAuth: string;
	olcrtcTransport: string;
	protocolFields: Record<string, unknown>;
	original?: string;
	originalRecord?: Inbound;
}

const EMPTY: InboundForm = {
	name: "",
	protocol: "hysteria2",
	transport: "udp",
	port: "443",
	enabled: true,
	masqueradeURL: "",
	fallbackRoot: "",
	olcrtcRoomID: "",
	password: "",
	profiles: [],
	naiveUsername: "",
	naivePassword: "",
	hysteria2Password: "",
	hysteria2Insecure: false,
	olcrtcAuth: "",
	olcrtcTransport: "",
	protocolFields: {},
};

export function InboundsPage() {
	const isAdmin = useIsAdmin();
	const { t } = useI18n();
	const qc = useQueryClient();
	const [error, setError] = useState<string | null>(null);
	const [issues, setIssues] = useState<ApiValidationIssue[] | null>(null);
	const [feedback, setFeedback] = useState<MutationFeedback | null>(null);
	const [editing, setEditing] = useState<string | null>(null); // name being edited
	const [creating, setCreating] = useState(false);
	const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
	const [form, setForm] = useState<InboundForm>(EMPTY);
	const [generateError, setGenerateError] = useState<string | null>(null);

	// generateFieldValue fills a dynamic protocol field marked with a
	// generateAction. Passwords/keys are produced client-side with a CSPRNG;
	// room ids are delegated to the backend room endpoint which knows the
	// provider's auto-room rules.
	async function generateFieldValue(field: ProtocolField) {
		setGenerateError(null);
		const action = field.generateAction;
		if (action === "password" || action === "hex64") {
			const bytes = new Uint8Array(action === "hex64" ? 32 : 16);
			crypto.getRandomValues(bytes);
			const value = Array.from(bytes, (b) =>
				b.toString(16).padStart(2, "0"),
			).join("");
			setForm((prev) => ({
				...prev,
				protocolFields: { ...prev.protocolFields, [field.key]: value },
			}));
			return;
		}
		if (action === "room") {
			try {
				const provider = String(
					form.protocolFields[field.generateActionField ?? ""] ??
						form.olcrtcAuth ??
						"",
				);
				const result = (await apiFetch(`/api/protocols/${form.protocol}/room`, {
					method: "POST",
					body: JSON.stringify({ provider }),
				})) as { roomID?: string };
				if (!result.roomID) {
					throw new Error("empty room response");
				}
				setForm((prev) => ({
					...prev,
					protocolFields: {
						...prev.protocolFields,
						[field.key]: result.roomID,
					},
				}));
			} catch {
				setGenerateError(t("inbounds.generateFailed"));
			}
			return;
		}
		setGenerateError(t("inbounds.generateFailed"));
	}

	const inbounds = useQuery<Inbound[]>({
		queryKey: ["inbounds", "all"],
		queryFn: () => apiFetch("/api/inbounds"),
	});
	// Attached clients per inbound (normalized clients read model).
	const protocolCatalog = useQuery<
		Array<{
			protocol: string;
			displayName: string;
			transports: string[];
			inboundFieldSchema?: ProtocolField[];
		}>
	>({
		queryKey: ["protocols"],
		queryFn: () => apiFetch("/api/protocols"),
	});

	useEffect(() => {
		if (!creating && editing == null) return;
		const schema =
			protocolCatalog.data?.find((p) => p.protocol === form.protocol)
				?.inboundFieldSchema ?? [];
		setForm((prev) => {
			const protocolFields = { ...prev.protocolFields };
			let changed = false;
			for (const field of schema) {
				if (field.default != null && protocolFields[field.key] == null) {
					protocolFields[field.key] = field.default;
					changed = true;
				}
			}
			return changed ? { ...prev, protocolFields } : prev;
		});
	}, [creating, editing, protocolCatalog.data, form.protocol]);

	const clients = useQuery<{ items?: ClientView[] } | ClientView[]>({
		queryKey: ["clients", "for-inbounds"],
		queryFn: () => apiFetch("/api/v1/clients?pageSize=500"),
	});
	const clientList: ClientView[] = Array.isArray(clients.data)
		? clients.data
		: (clients.data?.items ?? []);

	function attachedClients(inboundName: string): ClientView[] {
		return clientList.filter((c) => (c.inboundIds ?? []).includes(inboundName));
	}

	function invalidate() {
		void qc.invalidateQueries({ queryKey: ["inbounds"] });
		void qc.invalidateQueries({ queryKey: ["clients"] });
		void qc.invalidateQueries({ queryKey: ["apply"] });
	}
	function record(body: unknown) {
		const b = body as MutationFeedback | undefined;
		if (b && (b.revision || b.applyJob || typeof b.success === "boolean")) {
			setFeedback(b);
		}
	}

	// hasDynamicField reports whether the current protocol's schema renders a
	// dynamic field with the given key. Flat fallback inputs (masqueradeURL,
	// fallbackRoot, olcrtcRoomID) are hidden when the dynamic schema already
	// exposes the same key so the form does not render the field twice.
	function hasDynamicField(key: string): boolean {
		return (
			protocolCatalog.data
				?.find((p) => p.protocol === form.protocol)
				?.inboundFieldSchema?.some((f) => f.key === key) ?? false
		);
	}

	function toBody(f: InboundForm, keepName?: string) {
		const port = f.port === "" ? undefined : Number.parseInt(f.port, 10);
		const protocolFields: Record<string, unknown> = {
			...f.protocolFields,
			hysteria2Insecure: Object.hasOwn(
				f.protocolFields ?? {},
				"hysteria2Insecure",
			)
				? Boolean(f.protocolFields.hysteria2Insecure)
				: f.hysteria2Insecure,
		};
		const pick = (key: string, flat: string): unknown => {
			if (Object.hasOwn(protocolFields, key)) return protocolFields[key];
			if (flat !== "") return flat;
			return (f.originalRecord as Record<string, unknown> | undefined)?.[key];
		};
		const body: Record<string, unknown> = {
			name: keepName ?? f.name,
			protocol: f.protocol,
			transport: f.transport,
			enabled: f.enabled,
			protocolFields,
			password: f.password || f.originalRecord?.password || undefined,
			profiles: f.profiles.length
				? f.profiles
				: f.originalRecord?.profiles || undefined,
			naiveUsername: pick("naiveUsername", f.naiveUsername),
			naivePassword:
				f.naivePassword || f.originalRecord?.naivePassword || undefined,
			hysteria2Password:
				f.hysteria2Password || f.originalRecord?.hysteria2Password || undefined,
			hysteria2Insecure: protocolFields.hysteria2Insecure,
			olcrtcAuth: pick("olcrtcAuth", f.olcrtcAuth) || undefined,
			olcrtcTransport: pick("olcrtcTransport", f.olcrtcTransport) || undefined,
		};
		if (port != null) body.port = port;
		body.masqueradeURL = pick("masqueradeURL", f.masqueradeURL);
		body.fallbackRoot = pick("fallbackRoot", f.fallbackRoot);
		const room = pick("olcrtcRoomID", f.olcrtcRoomID);
		body.olcrtcRoomID = room ?? "";
		return body;
	}

	const create = useMutation({
		mutationFn: async (f: InboundForm) =>
			apiFetch("/api/inbounds", {
				method: "POST",
				body: JSON.stringify(toBody(f)),
			}),
		onSuccess: (data) => {
			setCreating(false);
			setForm(EMPTY);
			setError(null);
			setIssues(null);
			record(data);
			invalidate();
		},
		onError: (e) => {
			setIssues(e instanceof ApiError ? (e.issues ?? null) : null);
			setError(
				e instanceof ApiError ? e.message : t("inbounds.error.createFailed"),
			);
		},
	});

	const update = useMutation({
		mutationFn: async (f: InboundForm & { original: string }) =>
			apiFetch(`/api/inbounds/${encodeURIComponent(f.original)}`, {
				method: "PUT",
				body: JSON.stringify(toBody(f, f.original)),
			}),
		onSuccess: (data) => {
			setEditing(null);
			setError(null);
			setIssues(null);
			record(data);
			invalidate();
		},
		onError: (e) => {
			setIssues(e instanceof ApiError ? (e.issues ?? null) : null);
			setError(
				e instanceof ApiError ? e.message : t("inbounds.error.updateFailed"),
			);
		},
	});

	const remove = useMutation({
		mutationFn: async (name: string) =>
			apiFetch(`/api/inbounds/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: (data) => {
			setConfirmDelete(null);
			setError(null);
			setIssues(null);
			record(data);
			invalidate();
		},
		onError: (e) => {
			setIssues(e instanceof ApiError ? (e.issues ?? null) : null);
			setError(
				e instanceof ApiError ? e.message : t("inbounds.error.deleteFailed"),
			);
		},
	});

	function startCreate() {
		const proto = EMPTY.protocol;
		const schema =
			protocolCatalog.data?.find((p) => p.protocol === proto)
				?.inboundFieldSchema ?? [];
		const protocolFields: Record<string, unknown> = {};
		for (const field of schema) {
			if (field.default != null) protocolFields[field.key] = field.default;
		}
		setForm({ ...EMPTY, protocolFields });
		setCreating(true);
		setEditing(null);
	}
	function startEdit(ib: Inbound) {
		setForm({
			name: ib.name,
			protocol: ib.protocol,
			transport: ib.transport ?? "tcp",
			port: ib.port != null ? String(ib.port) : "",
			enabled: ib.enabled ?? true,
			masqueradeURL: ib.masqueradeURL ?? "",
			fallbackRoot: ib.fallbackRoot ?? "",
			olcrtcRoomID: ib.olcrtcRoomID ?? "",
			password: ib.password ?? "",
			profiles: ib.profiles ?? [],
			naiveUsername: ib.naiveUsername ?? "",
			naivePassword: ib.naivePassword ?? "",
			hysteria2Password: ib.hysteria2Password ?? "",
			// Prefer the flat value, but fall back to the protocolFields copy
			// (legacy panel / raw API created flat-only records); otherwise
			// the checkbox renders wrong and a save silently drops insecure
			// (audit #67/#119).
			hysteria2Insecure: Boolean(
				(ib as unknown as Record<string, unknown>).hysteria2Insecure ??
					ib.protocolFields?.hysteria2Insecure ??
					false,
			),
			olcrtcAuth: ib.olcrtcAuth ?? "",
			olcrtcTransport: ib.olcrtcTransport ?? "",
			protocolFields: { ...(ib.protocolFields ?? {}) },
			originalRecord: ib,
		});
		setEditing(ib.name);
		setCreating(false);
	}

	function cancelEditor() {
		setCreating(false);
		setEditing(null);
		setForm(EMPTY);
	}

	const formCard =
		creating || editing ? (
			<div className="card">
				<DialogTitle style={{ fontSize: 15 }}>
					{creating
						? t("inbounds.newInbound")
						: t("inbounds.edit", { name: editing ?? "" })}
				</DialogTitle>
				<div className="creation-dialog-fields" style={{ marginTop: 8 }}>
					<FormItem>
						<Label htmlFor="ib-name">{t("common.name")}</Label>
						<Input
							id="ib-name"
							value={form.name}
							disabled={!!editing}
							onChange={(e) => setForm({ ...form, name: e.target.value })}
						/>
					</FormItem>
					<FormItem>
						<Label htmlFor="ib-proto">{t("inbounds.protocol")}</Label>
						<Select
							id="ib-proto"
							value={form.protocol}
							onChange={(e) => {
								const nextProtocol = e.target.value;
								const nextMeta = protocolCatalog.data?.find(
									(p) => p.protocol === nextProtocol,
								);
								const nextTransports = nextMeta?.transports ?? [];
								const protocolFields: Record<string, unknown> = {
									...form.protocolFields,
								};
								for (const field of nextMeta?.inboundFieldSchema ?? []) {
									if (
										field.default != null &&
										protocolFields[field.key] == null
									) {
										protocolFields[field.key] = field.default;
									}
								}
								// Reset transport to the first transport the new
								// protocol supports (e.g. naiveproxy is tcp-only);
								// keeping the previous protocol's transport makes
								// creation fail with a 400.
								setForm({
									...form,
									protocol: nextProtocol,
									transport: nextTransports.includes(form.transport)
										? form.transport
										: (nextTransports[0] ?? ""),
									protocolFields,
								});
							}}
						>
							{(protocolCatalog.data ?? []).map((p) => (
								<option key={p.protocol} value={p.protocol}>
									{p.displayName}
								</option>
							))}
						</Select>
					</FormItem>
					<FormItem>
						<Label htmlFor="ib-trans">{t("inbounds.transport")}</Label>
						<Select
							id="ib-trans"
							value={form.transport}
							onChange={(e) => setForm({ ...form, transport: e.target.value })}
						>
							{(
								protocolCatalog.data?.find((p) => p.protocol === form.protocol)
									?.transports ?? []
							).map((transport) => (
								<option key={transport} value={transport}>
									{transport}
								</option>
							))}
						</Select>
					</FormItem>
					<FormItem>
						<Label htmlFor="ib-port">{t("inbounds.port")}</Label>
						<Input
							id="ib-port"
							inputMode="numeric"
							value={form.port}
							onChange={(e) => setForm({ ...form, port: e.target.value })}
						/>
					</FormItem>
					{!hasDynamicField("masqueradeURL") ? (
						<FormItem>
							<Label htmlFor="ib-masq">{t("inbounds.masqueradeURL")}</Label>
							<Input
								id="ib-masq"
								value={form.masqueradeURL}
								onChange={(e) =>
									setForm({ ...form, masqueradeURL: e.target.value })
								}
							/>
						</FormItem>
					) : null}
					{!hasDynamicField("fallbackRoot") ? (
						<FormItem>
							<Label htmlFor="ib-fb">{t("inbounds.fallbackRoot")}</Label>
							<Input
								id="ib-fb"
								value={form.fallbackRoot}
								onChange={(e) =>
									setForm({ ...form, fallbackRoot: e.target.value })
								}
							/>
						</FormItem>
					) : null}
					{(
						protocolCatalog.data?.find((p) => p.protocol === form.protocol)
							?.inboundFieldSchema ?? []
					).map((field) => {
						const value = form.protocolFields[field.key] ?? field.default ?? "";
						const setValue = (next: unknown) =>
							setForm({
								...form,
								protocolFields: { ...form.protocolFields, [field.key]: next },
							});
						if (field.type === "checkbox") {
							return (
								<Label
									key={field.key}
									htmlFor={`ib-field-${field.key}`}
									style={{ display: "flex", gap: 8, alignItems: "center" }}
								>
									<input
										id={`ib-field-${field.key}`}
										type="checkbox"
										checked={Boolean(value)}
										onChange={(e) => setValue(e.target.checked)}
									/>
									<span>{field.label}</span>
								</Label>
							);
						}
						return (
							<FormItem key={field.key}>
								<Label htmlFor={`ib-field-${field.key}`}>
									{field.label}
									{field.required ? " *" : ""}
								</Label>
								{field.type === "select" ? (
									<Select
										id={`ib-field-${field.key}`}
										value={String(value)}
										onChange={(e) => setValue(e.target.value)}
									>
										{(field.options ?? []).map((option) => (
											<option key={option.value} value={option.value}>
												{option.label}
											</option>
										))}
									</Select>
								) : (
									<Input
										id={`ib-field-${field.key}`}
										type={
											field.type === "password"
												? "password"
												: field.type === "number"
													? "number"
													: "text"
										}
										value={String(value)}
										onChange={(e) =>
											setValue(
												field.type === "number"
													? e.target.value === ""
														? undefined
														: Number(e.target.value)
													: e.target.value,
											)
										}
									/>
								)}
								{field.generateAction ? (
									<button
										type="button"
										className="btn btn-secondary"
										style={{ marginTop: 6, fontSize: 12 }}
										onClick={() => void generateFieldValue(field)}
									>
										{field.generateAction === "room"
											? t("inbounds.generateRoom")
											: t("inbounds.generatePassword")}
									</button>
								) : null}
							</FormItem>
						);
					})}

					{form.protocol === "olcrtc" && !hasDynamicField("olcrtcRoomID") ? (
						<FormItem>
							<Label htmlFor="ib-room">{t("inbounds.olcrtcRoomID")}</Label>
							<Input
								id="ib-room"
								value={form.olcrtcRoomID}
								onChange={(e) =>
									setForm({ ...form, olcrtcRoomID: e.target.value })
								}
							/>
						</FormItem>
					) : null}
				</div>
				<Label
					htmlFor="ib-enabled"
					style={{
						display: "flex",
						gap: 8,
						alignItems: "center",
						margin: "12px 0",
					}}
				>
					<input
						id="ib-enabled"
						type="checkbox"
						checked={form.enabled}
						onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
					/>
					<span>{t("common.enabled")}</span>
				</Label>
				<div className="creation-dialog-actions">
					<Button
						variant="primary"
						disabled={create.isPending || update.isPending}
						onClick={() => {
							if (creating) {
								create.mutate(form);
							} else if (editing) {
								update.mutate({ ...form, original: editing });
							}
						}}
					>
						{creating ? t("common.create") : t("common.save")}
					</Button>
					<Button onClick={cancelEditor}>{t("common.cancel")}</Button>
				</div>
			</div>
		) : null;

	return (
		<>
			<div className="card">
				<div style={{ display: "flex", alignItems: "center", gap: 12 }}>
					<h2 style={{ margin: 0, flex: 1 }}>{t("inbounds.title")}</h2>
					{isAdmin ? (
						<button
							type="button"
							className="btn btn-primary"
							onClick={startCreate}
						>
							{t("inbounds.newInbound")}
						</button>
					) : null}
				</div>
			</div>

			{error ? (
				<div className="card">
					<p className="form-error">{error}</p>
					{issues && issues.length > 0 ? (
						<ul className="muted" style={{ marginTop: 8, fontSize: 13 }}>
							{issues.map((iss) => (
								<li
									key={`${iss.field ?? iss.inboundId ?? iss.code ?? "issue"}`}
								>
									[{iss.severity ?? "info"}] {iss.field ? `${iss.field}: ` : ""}
									{iss.message ?? ""}
								</li>
							))}
						</ul>
					) : null}
				</div>
			) : null}

			{generateError ? (
				<div className="card">
					<p className="form-error">{generateError}</p>
				</div>
			) : null}

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
							{feedback.success === false
								? t("inbounds.applyFailed")
								: t("inbounds.saved")}
						</span>
						{feedback.revision ? (
							<span className="muted" style={{ fontSize: 13 }}>
								{t("inbounds.desiredRev")} {feedback.revision.desired ?? "—"} ·{" "}
								{t("inbounds.applied")} {feedback.revision.applied ?? "—"} ·{" "}
								{feedback.revision.state ?? ""}
							</span>
						) : null}
						{feedback.applyJob?.id ? (
							<span className="muted mono" style={{ fontSize: 12 }}>
								{t("inbounds.job")} {feedback.applyJob.id} (
								{feedback.applyJob.status})
							</span>
						) : null}
						<button
							type="button"
							className="btn"
							style={{ marginLeft: "auto" }}
							onClick={() => setFeedback(null)}
						>
							{t("inbounds.dismiss")}
						</button>
					</div>
				</div>
			) : null}

			<Dialog
				open={creating || editing !== null}
				onOpenChange={(open) => {
					if (!open) cancelEditor();
				}}
			>
				<DialogContent className="creation-dialog creation-dialog-wide">
					{formCard}
				</DialogContent>
			</Dialog>

			<div className="card">
				{inbounds.isLoading ? (
					<p className="muted">{t("common.loading")}</p>
				) : inbounds.isError ? (
					<FormMessage>
						{inbounds.error instanceof ApiError
							? inbounds.error.message
							: t("inbounds.error.loadFailed")}
					</FormMessage>
				) : (inbounds.data ?? []).length === 0 ? (
					<p className="muted">{t("inbounds.empty")}</p>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>{t("common.name")}</TableHead>
								<TableHead>{t("inbounds.protocol")}</TableHead>
								<TableHead>{t("inbounds.transport")}</TableHead>
								<TableHead>{t("inbounds.port")}</TableHead>
								<TableHead>{t("common.status")}</TableHead>
								<TableHead>{t("inbounds.attachedClients")}</TableHead>
								{isAdmin ? <TableHead>{t("common.actions")}</TableHead> : null}
							</TableRow>
						</TableHeader>
						<TableBody>
							{(inbounds.data ?? []).map((ib) => {
								const attached = attachedClients(ib.name);
								return (
									<TableRow key={ib.name}>
										<TableCell>{ib.name}</TableCell>
										<TableCell className="muted">{ib.protocol}</TableCell>
										<TableCell className="muted">
											{ib.transport ?? "—"}
										</TableCell>
										<TableCell className="muted">{ib.port ?? "—"}</TableCell>
										<TableCell>
											<Badge variant={ib.enabled ? "success" : "default"}>
												{ib.enabled
													? t("common.enabled")
													: t("common.disabled")}
											</Badge>
										</TableCell>
										<TableCell>
											{clients.isError ? (
												<span className="form-error">
													{clients.error instanceof ApiError
														? clients.error.message
														: t("inbounds.clientsUnavailable")}
												</span>
											) : attached.length === 0 ? (
												<span className="muted">—</span>
											) : (
												<span
													style={{
														display: "flex",
														gap: 4,
														flexWrap: "wrap",
													}}
												>
													{attached.map((c) => (
														<Link
															key={c.id}
															to="/clients/$clientId"
															params={{ clientId: c.id }}
														>
															<Badge>{c.name}</Badge>
														</Link>
													))}
												</span>
											)}
										</TableCell>
										{isAdmin ? (
											<TableCell>
												<div
													style={{
														display: "flex",
														gap: 6,
														flexWrap: "wrap",
													}}
												>
													<Button size="sm" onClick={() => startEdit(ib)}>
														{t("common.edit")}
													</Button>
													<Button
														size="sm"
														disabled={update.isPending}
														onClick={() =>
															update.mutate({
																name: ib.name,
																protocol: ib.protocol,
																transport: ib.transport ?? "tcp",
																port: ib.port != null ? String(ib.port) : "",
																enabled: !ib.enabled,
																masqueradeURL: ib.masqueradeURL ?? "",
																fallbackRoot: ib.fallbackRoot ?? "",
																olcrtcRoomID: ib.olcrtcRoomID ?? "",
																password: ib.password ?? "",
																profiles: ib.profiles ?? [],
																naiveUsername: ib.naiveUsername ?? "",
																naivePassword: ib.naivePassword ?? "",
																hysteria2Password: ib.hysteria2Password ?? "",
																hysteria2Insecure: Boolean(
																	(ib as unknown as Record<string, unknown>)
																		.hysteria2Insecure ??
																		ib.protocolFields?.hysteria2Insecure ??
																		false,
																),
																olcrtcAuth: ib.olcrtcAuth ?? "",
																olcrtcTransport: ib.olcrtcTransport ?? "",
																protocolFields: {
																	...(ib.protocolFields ?? {}),
																},
																originalRecord: ib,
																original: ib.name,
															})
														}
													>
														{ib.enabled
															? t("common.disable")
															: t("common.enable")}
													</Button>
													<Button
														size="sm"
														variant="danger"
														onClick={() => setConfirmDelete(ib.name)}
													>
														{t("common.delete")}
													</Button>
												</div>
											</TableCell>
										) : null}
									</TableRow>
								);
							})}
						</TableBody>
					</Table>
				)}
			</div>

			<AlertDialog
				open={confirmDelete !== null}
				onOpenChange={(open) => {
					if (!open) setConfirmDelete(null);
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>{t("inbounds.delete.title")}</AlertDialogTitle>
						<AlertDialogDescription>
							{t("inbounds.delete.description", { name: confirmDelete ?? "" })}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={remove.isPending}
							onClick={(e) => {
								e.preventDefault();
								if (confirmDelete) remove.mutate(confirmDelete);
							}}
						>
							{remove.isPending
								? t("inbounds.delete.deleting")
								: t("inbounds.delete.confirm")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</>
	);
}
