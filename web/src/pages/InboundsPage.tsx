import {
	useMutation,
	useQueries,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import {
	ApiError,
	type ApiValidationIssue,
	apiFetch,
	mutationErrorMessage,
} from "../api/fetcher";
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
import { createInbound } from "./inboundsCreateRecovery";

/** S4: mutation envelope feedback (revision/applyJob/success). */
interface MutationFeedback {
	revision?: { desired?: number; applied?: number; state?: string };
	applyJob?: { id?: string; status?: string };
	success?: boolean;
	/** reconciled marks an outcome recovered after a lost response (timeout). */
	reconciled?: boolean;
}

type ProtocolField = {
	key: string;
	label: string;
	type: string;
	required?: boolean;
	default?: unknown;
	options?: Array<{
		label: string;
		value: string;
		attributes?: Record<string, string>;
	}>;
	generateAction?: string;
	generateActionField?: string;
};

const OLCRTC_AUTH_FIELD = "olcrtcAuth";

function optionAutoRoom(
	schema: ProtocolField[],
	providerKey: string,
	provider: string,
): boolean | undefined {
	const field = schema.find((item) => item.key === providerKey);
	const raw = field?.options?.find((option) => option.value === provider)
		?.attributes?.["data-autoroom"];
	if (raw == null) return undefined;
	return raw === "true";
}

function roomProviderKey(field: ProtocolField): string {
	return field.generateActionField ?? OLCRTC_AUTH_FIELD;
}

function currentRoomProvider(form: InboundForm, field: ProtocolField): string {
	const key = roomProviderKey(field);
	return String(form.protocolFields[key] ?? form.olcrtcAuth ?? "");
}

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

function parsePort(value: string): number | undefined {
	const trimmed = value.trim();
	if (trimmed === "") return undefined;
	// Strict digits only — parseInt("12abc") truncates to 12 and would ship a
	// port the operator never typed; refuse non-integers instead (#1043).
	if (!PORT_DIGITS_PATTERN.test(trimmed)) return undefined;
	const port = Number(trimmed);
	return Number.isSafeInteger(port) ? port : undefined;
}

// Mirror of the server contract (internal/inbounds/inbound_validation.go):
// names are URL/catalog keys restricted to ^[A-Za-z0-9_-]+$, ports are
// integers in [1, 65535]. parseInt silently corrupts malformed input
// ("12abc" → 12), so the gate below rejects non-digits outright instead of
// letting them reach the wire as a different value or a raw 400 (#1043).
const INBOUND_NAME_PATTERN = /^[A-Za-z0-9_-]+$/;
const PORT_DIGITS_PATTERN = /^\d+$/;

interface InboundFieldErrors {
	name?: string;
	port?: string;
}

function schemaFieldDefault(
	field: ProtocolField,
	port: string,
	settingsPort?: number,
): unknown {
	if (field.key === "publicPort") {
		return (
			parsePort(port) ??
			(settingsPort != null && settingsPort > 0 ? settingsPort : field.default)
		);
	}
	return field.default;
}

function applyCreateDefaults(
	schema: ProtocolField[],
	protocolFields: Record<string, unknown>,
	port: string,
	settingsPort?: number,
): { protocolFields: Record<string, unknown>; changed: boolean } {
	const next = { ...protocolFields };
	let changed = false;
	for (const field of schema) {
		if (field.default != null && next[field.key] == null) {
			next[field.key] = schemaFieldDefault(field, port, settingsPort);
			changed = true;
		}
	}
	return { protocolFields: next, changed };
}

function livePortValue(ib: Inbound): string {
	if (ib.port != null) return String(ib.port);
	const publicPort = ib.protocolFields?.publicPort;
	if (typeof publicPort === "number" && Number.isFinite(publicPort)) {
		return String(publicPort);
	}
	if (typeof publicPort === "string" && publicPort !== "") {
		return publicPort;
	}
	return "";
}

// Schema-declared fields with a flat record counterpart must agree: the
// server resolves the protocolFields copy first, so loading a flat-only
// record without seeding would show the schema DEFAULT in the schema input
// while the live value hides in the flat field — a save would then ship a
// pair that disagrees (#850). Password fields are excluded: their flat copy
// can carry the "[REDACTED]" echo the server's preserve-redacted path
// restores, and seeding it would surface the sentinel in the input.
// publicPort is excluded too — it is resolved via livePortValue/port.
function seededRecordFields(
	ib: Inbound,
	schema: ProtocolField[],
): Record<string, unknown> {
	const fields: Record<string, unknown> = { ...(ib.protocolFields ?? {}) };
	const flat = ib as unknown as Record<string, unknown>;
	for (const field of schema) {
		if (
			field.key === "publicPort" ||
			field.type === "password" ||
			field.type === "checkbox"
		) {
			continue;
		}
		if (Object.hasOwn(fields, field.key)) continue;
		const value = flat[field.key];
		if (value != null && value !== "") {
			fields[field.key] = value;
		}
	}
	return fields;
}

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
	// #709: row enable/disable drops live access for every attached client —
	// gate it like the Delete next to it.
	const [confirmToggle, setConfirmToggle] = useState<Inbound | null>(null);
	const [form, setForm] = useState<InboundForm>(EMPTY);
	const [fieldErrors, setFieldErrors] = useState<InboundFieldErrors>({});
	const [generateError, setGenerateError] = useState<string | null>(null);

	function clearFieldError(key: keyof InboundFieldErrors) {
		setFieldErrors((prev) =>
			prev[key] === undefined ? prev : { ...prev, [key]: undefined },
		);
	}

	// Client-side gate for the name/port contract — without it a bad value
	// sails to a raw server 400, or worse: parseInt("12abc") silently ships a
	// DIFFERENT port than the operator typed (#1043).
	function validateForm(f: InboundForm): boolean {
		const next: InboundFieldErrors = {};
		if (!f.name.trim()) {
			next.name = t("inbounds.validation.nameRequired");
		} else if (!INBOUND_NAME_PATTERN.test(f.name.trim())) {
			next.name = t("inbounds.validation.nameCharset");
		}
		const port = f.port.trim();
		if (!PORT_DIGITS_PATTERN.test(port)) {
			next.port = t("inbounds.validation.portRange");
		} else {
			const n = Number(port);
			if (!Number.isSafeInteger(n) || n < 1 || n > 65535) {
				next.port = t("inbounds.validation.portRange");
			}
		}
		setFieldErrors(next);
		return next.name === undefined && next.port === undefined;
	}

	function submitForm() {
		setError(null);
		setIssues(null);
		if (!validateForm(form)) return;
		if (creating) {
			create.mutate(form);
		} else if (editing) {
			update.mutate({ ...form, original: editing });
		}
	}

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
			const providerKey = roomProviderKey(field);
			const provider = currentRoomProvider(form, field);
			const schema =
				protocolCatalog.data?.find((p) => p.protocol === form.protocol)
					?.inboundFieldSchema ?? [];
			if (optionAutoRoom(schema, providerKey, provider) === false) {
				setGenerateError(t("inbounds.generateRoomManual"));
				return;
			}
			try {
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
	const settings = useQuery<{ defaultInboundPublicPort?: number }>({
		queryKey: ["settings"],
		queryFn: () => apiFetch("/api/settings"),
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
		if (!creating) return;
		const schema =
			protocolCatalog.data?.find((p) => p.protocol === form.protocol)
				?.inboundFieldSchema ?? [];
		const settingsPort = settings.data?.defaultInboundPublicPort;
		setForm((prev) => {
			const prefillPort =
				settingsPort != null &&
				settingsPort > 0 &&
				(prev.port === "" || prev.port === EMPTY.port)
					? String(settingsPort)
					: prev.port;
			const { protocolFields, changed } = applyCreateDefaults(
				schema,
				prev.protocolFields,
				prefillPort,
				settingsPort,
			);
			if (!changed && prefillPort === prev.port) return prev;
			return { ...prev, port: prefillPort, protocolFields };
		});
	}, [creating, protocolCatalog.data, form.protocol, settings.data]);

	const inboundItems = inbounds.data ?? [];
	const attachedQueries = useQueries({
		queries: inboundItems.map((ib) => ({
			queryKey: ["clients", "for-inbound", ib.name],
			queryFn: () =>
				apiFetch(
					`/api/inbounds/${encodeURIComponent(ib.name)}/clients?pageSize=500`,
				) as Promise<{ items?: ClientView[]; total?: number }>,
		})),
	});
	// DELETE 409s when bindings still reference the inbound — the confirm
	// dialog must not promise a detach cascade that does not exist (#712).
	const confirmDeleteIndex = inboundItems.findIndex(
		(ib) => ib.name === confirmDelete,
	);
	const confirmDeleteQuery =
		confirmDeleteIndex >= 0 ? attachedQueries[confirmDeleteIndex] : undefined;
	const confirmDeleteAttached = confirmDeleteQuery?.data?.items ?? [];
	const confirmDeleteAttachedTotal =
		confirmDeleteQuery?.data?.total ?? confirmDeleteAttached.length;
	const confirmDeleteBlocked = confirmDeleteAttachedTotal > 0;
	// While the attached-clients query is loading or errored the binding count
	// is unknown — withhold the destructive confirm rather than let a stale
	// "no attachments" read slip through to the fail-closed 409.
	const confirmDeleteKnown = confirmDeleteQuery?.isSuccess === true;

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
		const port = parsePort(f.port);
		const schema =
			protocolCatalog.data?.find((p) => p.protocol === f.protocol)
				?.inboundFieldSchema ?? [];
		const hasPublicPort = schema.some((field) => field.key === "publicPort");
		const protocolFields: Record<string, unknown> = {
			...f.protocolFields,
			hysteria2Insecure: Object.hasOwn(
				f.protocolFields ?? {},
				"hysteria2Insecure",
			)
				? Boolean(f.protocolFields.hysteria2Insecure)
				: f.hysteria2Insecure,
		};
		if (port != null && hasPublicPort) {
			protocolFields.publicPort = port;
		}
		const pick = (key: string, flat: string): unknown => {
			if (Object.hasOwn(protocolFields, key)) return protocolFields[key];
			if (flat !== "") return flat;
			return (f.originalRecord as Record<string, unknown> | undefined)?.[key];
		};
		// Schema fields with a flat counterpart must be echoed in BOTH places:
		// the server resolves the protocolFields copy first, so a flat-only
		// record saved without it would leave the two representations
		// inconsistent (and the schema input would have shown the field
		// default, not the live flat value). Password fields stay flat-only —
		// their flat value may carry the "[REDACTED]" echo that the server's
		// preserve-redacted path restores (#850).
		for (const field of schema) {
			if (
				field.key === "publicPort" ||
				field.type === "password" ||
				field.type === "checkbox"
			) {
				continue;
			}
			if (Object.hasOwn(protocolFields, field.key)) continue;
			const flat = (f as unknown as Record<string, unknown>)[field.key];
			const resolved =
				typeof flat === "string" && flat !== ""
					? flat
					: (f.originalRecord as Record<string, unknown> | undefined)?.[
							field.key
						];
			if (resolved != null && resolved !== "") {
				protocolFields[field.key] = resolved;
			}
		}
		const body: Record<string, unknown> = {
			name: keepName ?? f.name.trim(),
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
		mutationFn: async (f: InboundForm): Promise<MutationFeedback> =>
			// Retried under a stable Idempotency-Key and reconciled by the
			// committed object so a lost response cannot strand the operator on
			// a retryable Create form for an already-existing inbound (audit
			// #312).
			createInbound(toBody(f), f.name),
		onSuccess: (data) => {
			setIssues(null);
			record(data);
			invalidate();
			if (data?.success === false) {
				// The inbound committed but the auto-apply failed — keep the
				// editor open and say so instead of dismissing it as a clean
				// create (#649).
				setError(t("inbounds.error.applyFailed"));
				return;
			}
			setCreating(false);
			setForm(EMPTY);
			setError(null);
		},
		onError: (e) => {
			setIssues(e instanceof ApiError ? (e.issues ?? null) : null);
			setError(mutationErrorMessage(e, t("inbounds.error.createFailed"), t));
		},
	});

	const update = useMutation({
		mutationFn: async (f: InboundForm & { original: string }) =>
			apiFetch(`/api/inbounds/${encodeURIComponent(f.original)}`, {
				method: "PUT",
				body: JSON.stringify(toBody(f, f.original)),
			}),
		onSuccess: (data) => {
			setIssues(null);
			record(data);
			invalidate();
			if ((data as MutationFeedback | undefined)?.success === false) {
				// Committed but apply failed — keep the editor open with the
				// failure visible rather than dismissing it as a save (#649).
				setError(t("inbounds.error.applyFailed"));
				return;
			}
			setEditing(null);
			setConfirmToggle(null);
			setError(null);
		},
		onError: (e) => {
			setIssues(e instanceof ApiError ? (e.issues ?? null) : null);
			setError(mutationErrorMessage(e, t("inbounds.error.updateFailed"), t));
		},
	});

	const remove = useMutation({
		mutationFn: async (name: string) =>
			apiFetch(`/api/inbounds/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: (data) => {
			setIssues(null);
			record(data);
			invalidate();
			if ((data as MutationFeedback | undefined)?.success === false) {
				// The delete committed but the apply failed — keep the confirm
				// dialog open with the failure shown instead of dismissing it
				// as a clean delete (#649).
				setError(t("inbounds.error.applyFailed"));
				return;
			}
			setConfirmDelete(null);
			setError(null);
		},
		onError: (e) => {
			setIssues(e instanceof ApiError ? (e.issues ?? null) : null);
			setError(mutationErrorMessage(e, t("inbounds.error.deleteFailed"), t));
		},
	});

	function startCreate() {
		const proto = EMPTY.protocol;
		const schema =
			protocolCatalog.data?.find((p) => p.protocol === proto)
				?.inboundFieldSchema ?? [];
		const settingsPort = settings.data?.defaultInboundPublicPort;
		const port =
			settingsPort != null && settingsPort > 0
				? String(settingsPort)
				: EMPTY.port;
		const { protocolFields } = applyCreateDefaults(
			schema,
			{},
			port,
			settingsPort,
		);
		setForm({ ...EMPTY, port, protocolFields });
		setFieldErrors({});
		setCreating(true);
		setEditing(null);
	}
	function startEdit(ib: Inbound) {
		setForm({
			name: ib.name,
			protocol: ib.protocol,
			transport: ib.transport ?? "tcp",
			port: livePortValue(ib),
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
			// #850: seed the schema-keyed copies from the flat record fields so
			// the schema inputs show the live values (not defaults) and a save
			// echoes both representations with the same value.
			protocolFields: seededRecordFields(
				ib,
				protocolCatalog.data?.find((p) => p.protocol === ib.protocol)
					?.inboundFieldSchema ?? [],
			),
			originalRecord: ib,
		});
		setFieldErrors({});
		setEditing(ib.name);
		setCreating(false);
	}

	function cancelEditor() {
		setCreating(false);
		setEditing(null);
		setForm(EMPTY);
		setFieldErrors({});
	}

	// Row enable/disable fires a full PUT echoing the list record — the same
	// payload the button used to send one-click (#709 now confirms first).
	function toggleInbound(ib: Inbound) {
		update.mutate({
			name: ib.name,
			protocol: ib.protocol,
			transport: ib.transport ?? "tcp",
			// #717: same effective-port resolution as the edit form — schema
			// inbounds carry it in protocolFields.publicPort, not flat port.
			port: livePortValue(ib),
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
				(ib as unknown as Record<string, unknown>).hysteria2Insecure ??
					ib.protocolFields?.hysteria2Insecure ??
					false,
			),
			olcrtcAuth: ib.olcrtcAuth ?? "",
			olcrtcTransport: ib.olcrtcTransport ?? "",
			// #850: same flat→protocolFields seeding as the edit form so the
			// toggle PUT echoes the live value in both representations.
			protocolFields: seededRecordFields(
				ib,
				protocolCatalog.data?.find((p) => p.protocol === ib.protocol)
					?.inboundFieldSchema ?? [],
			),
			originalRecord: ib,
			original: ib.name,
		});
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
							aria-invalid={fieldErrors.name ? true : undefined}
							onChange={(e) => {
								setForm({ ...form, name: e.target.value });
								clearFieldError("name");
							}}
						/>
						{fieldErrors.name ? (
							<FormMessage>{fieldErrors.name}</FormMessage>
						) : null}
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
								const { protocolFields } = creating
									? applyCreateDefaults(
											nextMeta?.inboundFieldSchema ?? [],
											form.protocolFields,
											form.port,
											settings.data?.defaultInboundPublicPort,
										)
									: { protocolFields: { ...form.protocolFields } };
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
							aria-invalid={fieldErrors.port ? true : undefined}
							onChange={(e) => {
								const port = e.target.value;
								const nextFields = { ...form.protocolFields };
								if (hasDynamicField("publicPort")) {
									const parsed = parsePort(port);
									if (parsed == null) {
										delete nextFields.publicPort;
									} else {
										nextFields.publicPort = parsed;
									}
								}
								setForm({ ...form, port, protocolFields: nextFields });
								clearFieldError("port");
							}}
						/>
						{fieldErrors.port ? (
							<FormMessage>{fieldErrors.port}</FormMessage>
						) : null}
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
					)
						.filter((field) => field.key !== "publicPort")
						.map((field) => {
							const schema =
								protocolCatalog.data?.find((p) => p.protocol === form.protocol)
									?.inboundFieldSchema ?? [];
							const value =
								form.protocolFields[field.key] ?? field.default ?? "";
							const setValue = (next: unknown) =>
								setForm({
									...form,
									protocolFields: { ...form.protocolFields, [field.key]: next },
								});
							const roomManual =
								field.generateAction === "room" &&
								optionAutoRoom(
									schema,
									roomProviderKey(field),
									currentRoomProvider(form, field),
								) === false;
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
											disabled={roomManual}
											title={
												roomManual
													? t("inbounds.generateRoomManual")
													: undefined
											}
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
						onClick={submitForm}
					>
						{creating ? t("common.create") : t("common.save")}
					</Button>
					<Button onClick={cancelEditor}>{t("common.cancel")}</Button>
				</div>
				{/* While this dialog is open the page-level error card is hidden
					behind the overlay — repeat it here so a failed apply stays
					visible in context (#649). */}
				{error ? <p className="form-error">{error}</p> : null}
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
						{feedback.reconciled ? (
							<span className="muted" style={{ fontSize: 13 }}>
								{t("inbounds.createReconciled")}
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
							{(inbounds.data ?? []).map((ib, inboundIndex) => {
								const attachedQuery = attachedQueries[inboundIndex];
								const attached = attachedQuery?.data?.items ?? [];
								const attachedTotal =
									attachedQuery?.data?.total ?? attached.length;
								return (
									<TableRow key={ib.name}>
										<TableCell>{ib.name}</TableCell>
										<TableCell className="muted">{ib.protocol}</TableCell>
										<TableCell className="muted">
											{ib.transport ?? "—"}
										</TableCell>
										{/* #717: the effective port may live in
										 * protocolFields.publicPort (schema
										 * protocols like naive) — render what the
										 * edit form would use. */}
										<TableCell className="muted">
											{livePortValue(ib) || "—"}
										</TableCell>
										<TableCell>
											<Badge variant={ib.enabled ? "success" : "default"}>
												{ib.enabled
													? t("common.enabled")
													: t("common.disabled")}
											</Badge>
										</TableCell>
										<TableCell>
											{attachedQuery?.isError ? (
												<span className="form-error">
													{attachedQuery.error instanceof ApiError
														? attachedQuery.error.message
														: t("inbounds.clientsUnavailable")}
												</span>
											) : attachedTotal === 0 ? (
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
													{attachedTotal > attached.length ? (
														<span className="muted">
															+{attachedTotal - attached.length}
														</span>
													) : null}
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
														onClick={() => {
															setError(null);
															setConfirmToggle(ib);
														}}
													>
														{ib.enabled
															? t("common.disable")
															: t("common.enable")}
													</Button>
													<Button
														size="sm"
														variant="danger"
														onClick={() => {
															setError(null);
															setConfirmDelete(ib.name);
														}}
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
							{!confirmDeleteKnown
								? confirmDeleteQuery?.isError
									? t("inbounds.clientsUnavailable")
									: t("inbounds.delete.checking")
								: confirmDeleteBlocked
									? t("inbounds.delete.blocked", {
											name: confirmDelete ?? "",
											count: confirmDeleteAttachedTotal,
										})
									: t("inbounds.delete.description", {
											name: confirmDelete ?? "",
										})}
						</AlertDialogDescription>
					</AlertDialogHeader>
					{confirmDeleteBlocked ? (
						<div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
							{confirmDeleteAttached.map((client) => (
								<Link
									key={client.id}
									to="/clients/$clientId"
									params={{ clientId: client.id }}
								>
									<Badge>{client.name}</Badge>
								</Link>
							))}
							{confirmDeleteAttachedTotal > confirmDeleteAttached.length ? (
								<span className="muted">
									+{confirmDeleteAttachedTotal - confirmDeleteAttached.length}
								</span>
							) : null}
						</div>
					) : null}
					{/* A committed-but-unapplied delete keeps this dialog open —
						the failure must be visible here, not only behind the
						overlay (#649). */}
					{error ? <p className="form-error">{error}</p> : null}
					<AlertDialogFooter>
						<AlertDialogCancel>
							{confirmDeleteBlocked || !confirmDeleteKnown
								? t("common.close")
								: t("common.cancel")}
						</AlertDialogCancel>
						{confirmDeleteBlocked || !confirmDeleteKnown ? null : (
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
						)}
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>

			{/* #709: enable/disable drops live access for every attached client
				and kicks auto-apply — confirm naming the inbound, like Delete. */}
			<AlertDialog
				open={confirmToggle !== null}
				onOpenChange={(open) => {
					if (!open) setConfirmToggle(null);
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>
							{confirmToggle?.enabled
								? t("inbounds.toggle.disableTitle")
								: t("inbounds.toggle.enableTitle")}
						</AlertDialogTitle>
						<AlertDialogDescription>
							{confirmToggle?.enabled
								? t("inbounds.toggle.disableDescription", {
										name: confirmToggle.name,
									})
								: t("inbounds.toggle.enableDescription", {
										name: confirmToggle?.name ?? "",
									})}
						</AlertDialogDescription>
					</AlertDialogHeader>
					{/* A committed-but-unapplied/failed toggle keeps this dialog
						open — the failure must be visible here, not only behind
						the overlay (#649). */}
					{error ? <p className="form-error">{error}</p> : null}
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={update.isPending}
							onClick={(e) => {
								e.preventDefault();
								if (confirmToggle) toggleInbound(confirmToggle);
							}}
						>
							{update.isPending
								? t("inbounds.toggle.pending")
								: confirmToggle?.enabled
									? t("inbounds.toggle.confirmDisable")
									: t("inbounds.toggle.confirmEnable")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</>
	);
}
