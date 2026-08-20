import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { ApiError, apiFetch } from "../api/fetcher";
import {
	deleteApiV1ClientsId,
	deleteApiV1ClientsIdBindingsBindingId,
	patchApiV1ClientsId,
	patchApiV1ClientsIdBindingsBindingId,
	postApiV1ClientsIdBindings,
	postApiV1ClientsIdCredentialsBindingIdRotate,
} from "../api/generated/clients/clients";
import type { BindingView, ClientView } from "../api/generated/models";
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
	AlertDialogTrigger,
} from "../components/ui/alert-dialog";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { FormDescription, FormItem, FormMessage } from "../components/ui/form";
import { Input, Textarea } from "../components/ui/input";
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
import { Tabs, TabsList, TabsTrigger } from "../components/ui/tabs";
import { useI18n } from "../i18n/I18nContext";
import {
	decimalWithinSafeInteger,
	fmtBytes,
	parseQuotaDecimal,
} from "../lib/bytes";
import { ClientConnectionLinks } from "../subscription/ClientConnectionLinks";
import { ClientTrafficPanel } from "../subscription/ClientTrafficPanel";
import { SubscriptionTokensPanel } from "../subscription/SubscriptionTokensPanel";

type ClientDetail = ClientView;
type Tab = "overview" | "access" | "subscription" | "traffic" | "audit";

/** S3: revision/apply feedback returned by every mutation envelope. */
interface MutationFeedback {
	revision?: { desired?: number; applied?: number; state?: string };
	applyJob?: { id?: string; status?: string; desiredRevision?: number };
	success?: boolean;
}

/** S3: client edit form — RHF + Zod. quotaBytes is kept as a decimal string in
 * the form and converted with integer parsing (never Number() on raw bytes)
 * to avoid float precision loss on large quotas. Built per-render so the
 * validation messages follow the active locale. */
function buildEditSchema(t: (key: string) => string) {
	return z.object({
		name: z.string().min(1, t("clientDetail.validation.nameRequired")),
		email: z
			.string()
			.email(t("clientDetail.validation.invalidEmail"))
			.or(z.literal("")),
		enabled: z.boolean(),
		quotaBytes: z
			.string()
			.refine(
				(v) => v === "" || /^\d+$/.test(v),
				t("clientDetail.validation.wholeBytes"),
			)
			.refine(
				// Issue 3: reject values the JSON/API round-trip cannot
				// represent exactly (Number.MAX_SAFE_INTEGER). Compare as
				// decimal strings to stay exact beyond float precision.
				(v) => v === "" || decimalWithinSafeInteger(v),
				t("clientDetail.validation.quotaTooLarge"),
			),
		expiresAt: z.string(),
		notes: z.string(),
	});
}
type EditValues = z.infer<ReturnType<typeof buildEditSchema>>;

interface InboundOption {
	name: string;
	protocol: string;
	enabled?: boolean;
}

interface AuditEntry {
	id?: string;
	action?: string;
	target?: string;
	actor?: string;
	success?: boolean;
	timestamp?: string;
	details?: unknown;
}

export function ClientDetailPage() {
	const { clientId } = useParams({ strict: false }) as { clientId: string };
	const isAdmin = useIsAdmin();
	const navigate = useNavigate();
	const qc = useQueryClient();
	const [tab, setTab] = useState<Tab>("overview");
	const [revealed, setRevealed] = useState<Record<string, string>>({});
	const [error, setError] = useState<string | null>(null);
	const [feedback, setFeedback] = useState<MutationFeedback | null>(null);
	const [attachInbound, setAttachInbound] = useState("");
	const { t } = useI18n();

	// Clear one-time revealed credentials on unmount/navigation.
	useEffect(() => () => setRevealed({}), []);

	const client = useQuery<ClientDetail>({
		queryKey: ["clients", clientId],
		queryFn: () => apiFetch(`/api/v1/clients/${clientId}`),
	});

	const inbounds = useQuery<{ items?: InboundOption[] } | InboundOption[]>({
		queryKey: ["inbounds", "all"],
		queryFn: () => apiFetch("/api/inbounds"),
	});
	const inboundList: InboundOption[] = Array.isArray(inbounds.data)
		? inbounds.data
		: (inbounds.data?.items ?? []);

	const audit = useQuery<{ items?: AuditEntry[] } | AuditEntry[]>({
		queryKey: ["clients", clientId, "audit"],
		queryFn: () => apiFetch(`/api/v1/clients/${clientId}/audit`),
		enabled: tab === "audit",
	});
	const auditItems: AuditEntry[] = Array.isArray(audit.data)
		? audit.data
		: (audit.data?.items ?? []);

	function recordFeedback(body: unknown) {
		const b = body as MutationFeedback | undefined;
		if (b && (b.revision || b.applyJob || typeof b.success === "boolean")) {
			setFeedback(b);
		}
	}

	// S3: edit form (RHF + Zod), populated from the loaded client.
	const form = useForm<EditValues>({
		resolver: zodResolver(buildEditSchema(t)),
		defaultValues: {
			name: "",
			email: "",
			enabled: true,
			quotaBytes: "",
			expiresAt: "",
			notes: "",
		},
	});
	useEffect(() => {
		const c = client.data;
		if (!c) return;
		const notes = (c as { notes?: string }).notes ?? "";
		form.reset({
			name: c.name ?? "",
			email: c.email ?? "",
			enabled: c.enabled ?? true,
			// Bytes as a decimal string; never through Number() lossy input paths.
			quotaBytes: c.quotaBytes != null ? String(c.quotaBytes) : "",
			expiresAt: c.expiresAt
				? new Date(c.expiresAt * 1000).toISOString().slice(0, 10)
				: "",
			notes,
		});
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [client.data, form.reset]);

	const invalidate = () => {
		void qc.invalidateQueries({ queryKey: ["clients", clientId] });
		void qc.invalidateQueries({ queryKey: ["clients"] });
		void qc.invalidateQueries({ queryKey: ["apply"] });
	};

	const save = useMutation({
		mutationFn: async (v: EditValues) => {
			const c = client.data;
			if (!c) throw new Error("client not loaded");
			// Integer-parse the byte string; empty means clear/unlimited. The
			// Zod schema already rejected anything above MAX_SAFE_INTEGER, so
			// this conversion is exact.
			const quota =
				v.quotaBytes === "" ? undefined : parseQuotaDecimal(v.quotaBytes);
			const expires = v.expiresAt
				? Math.floor(new Date(v.expiresAt).getTime() / 1000)
				: undefined;
			const res = await patchApiV1ClientsId(clientId, {
				version: c.version ?? 0,
				...(form.getFieldState("name").isDirty ? { name: v.name } : {}),
				...(form.getFieldState("email").isDirty
					? { email: v.email || null }
					: {}),
				...(form.getFieldState("enabled").isDirty
					? { enabled: v.enabled }
					: {}),
				...(form.getFieldState("quotaBytes").isDirty
					? { quotaBytes: quota ?? null }
					: {}),
				...(form.getFieldState("expiresAt").isDirty
					? { expiresAt: expires ?? null }
					: {}),
				...(form.getFieldState("notes").isDirty
					? { notes: v.notes || null }
					: {}),
			});
			return res as unknown as MutationFeedback;
		},
		onSuccess: (data) => {
			setError(null);
			recordFeedback(data);
			invalidate();
		},
		onError: (err) =>
			setError(
				err instanceof ApiError ? err.message : t("clientDetail.error.save"),
			),
	});

	const enableToggle = useMutation({
		mutationFn: async () => {
			const c = client.data;
			if (!c) throw new Error("client not loaded");
			const res = await patchApiV1ClientsId(clientId, {
				enabled: !c.enabled,
				version: c.version ?? 0,
			});
			return res as unknown as MutationFeedback;
		},
		onSuccess: (data) => {
			setError(null);
			recordFeedback(data);
			invalidate();
		},
		onError: (err) =>
			setError(
				err instanceof ApiError ? err.message : t("clientDetail.error.update"),
			),
	});

	const remove = useMutation({
		mutationFn: async () => {
			const res = await deleteApiV1ClientsId(clientId);
			return res as unknown as MutationFeedback;
		},
		onSuccess: () => {
			void qc.invalidateQueries({ queryKey: ["clients"] });
			void navigate({ to: "/clients" });
		},
		onError: (err) =>
			setError(
				err instanceof ApiError ? err.message : t("clientDetail.error.delete"),
			),
	});

	const rotate = useMutation({
		mutationFn: async (bindingId: string) => {
			const res = await postApiV1ClientsIdCredentialsBindingIdRotate(
				clientId,
				bindingId,
				{},
			);
			// The backend envelope merges plaintext + revision/applyJob/success.
			return res as unknown as MutationFeedback & { plaintext?: string };
		},
		onSuccess: (res, bindingId) => {
			if (res.plaintext) {
				setRevealed((prev) => ({
					...prev,
					[bindingId]: res.plaintext as string,
				}));
			}
			setError(null);
			recordFeedback(res);
			invalidate();
		},
		onError: (err) =>
			setError(
				err instanceof ApiError ? err.message : t("clientDetail.error.rotate"),
			),
	});

	const toggleBinding = useMutation({
		mutationFn: async (b: BindingView) => {
			const res = await patchApiV1ClientsIdBindingsBindingId(clientId, b.id, {
				enabled: !b.enabled,
				version: b.version ?? 0,
			});
			return res as unknown as MutationFeedback;
		},
		onSuccess: (data) => {
			setError(null);
			recordFeedback(data);
			invalidate();
		},
		onError: (err) =>
			setError(
				err instanceof ApiError ? err.message : t("clientDetail.error.update"),
			),
	});

	const attach = useMutation({
		mutationFn: async (inboundId: string) => {
			const res = await postApiV1ClientsIdBindings(clientId, { inboundId });
			return res as unknown as MutationFeedback;
		},
		onSuccess: (data) => {
			setAttachInbound("");
			setError(null);
			recordFeedback(data);
			invalidate();
		},
		onError: (err) =>
			setError(
				err instanceof ApiError ? err.message : t("clientDetail.error.attach"),
			),
	});

	const detach = useMutation({
		mutationFn: async (bindingId: string) => {
			const res = await deleteApiV1ClientsIdBindingsBindingId(
				clientId,
				bindingId,
			);
			return res as unknown as MutationFeedback;
		},
		onSuccess: (data) => {
			setError(null);
			recordFeedback(data);
			invalidate();
		},
		onError: (err) =>
			setError(
				err instanceof ApiError ? err.message : t("clientDetail.error.detach"),
			),
	});

	if (client.isLoading) {
		return (
			<div className="card">
				<p className="muted">{t("common.loading")}</p>
			</div>
		);
	}
	if (client.isError || !client.data) {
		return (
			<div className="card">
				<p className="form-error">
					{client.error instanceof ApiError
						? client.error.message
						: t("clientDetail.error.load")}
				</p>
			</div>
		);
	}

	const c = client.data;
	const boundInboundIds = new Set((c.bindings ?? []).map((b) => b.inboundId));
	const attachable = inboundList.filter((ib) => !boundInboundIds.has(ib.name));
	const tabs: { id: Tab; label: string }[] = [
		{ id: "overview", label: t("clientDetail.tab.overview") },
		{ id: "access", label: t("clientDetail.tab.access") },
		{ id: "subscription", label: t("clientDetail.tab.subscription") },
		{ id: "traffic", label: t("nav.traffic") },
		{ id: "audit", label: t("clientDetail.tab.audit") },
	];

	return (
		<>
			<div className="card">
				<div className="h-scroll" style={{ gap: 12 }}>
					<h2 style={{ margin: 0, flex: 1 }}>{c.name}</h2>
					{isAdmin ? (
						<>
							<Button
								variant="default"
								disabled={enableToggle.isPending}
								onClick={() => enableToggle.mutate()}
							>
								{c.enabled
									? t("clientDetail.disableClient")
									: t("clientDetail.enableClient")}
							</Button>
							<AlertDialog>
								<AlertDialogTrigger asChild>
									<Button variant="danger">{t("common.delete")}</Button>
								</AlertDialogTrigger>
								<AlertDialogContent>
									<AlertDialogHeader>
										<AlertDialogTitle>
											{t("clientDetail.deleteTitle")}
										</AlertDialogTitle>
										<AlertDialogDescription>
											{t("clientDetail.deleteDescription")}
										</AlertDialogDescription>
									</AlertDialogHeader>
									<AlertDialogFooter>
										<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
										<AlertDialogAction
											disabled={remove.isPending}
											onClick={() => remove.mutate()}
										>
											{t("clientDetail.confirmDelete")}
										</AlertDialogAction>
									</AlertDialogFooter>
								</AlertDialogContent>
							</AlertDialog>
						</>
					) : null}
				</div>
				<Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
					<TabsList style={{ marginTop: 12 }}>
						{tabs.map((t) => (
							<TabsTrigger key={t.id} value={t.id}>
								{t.label}
							</TabsTrigger>
						))}
					</TabsList>
				</Tabs>
			</div>

			{error ? (
				<div className="card">
					<p className="form-error">{error}</p>
				</div>
			) : null}

			{/* S3: revision/apply feedback surfaced after each mutation */}
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
						<Badge variant={feedback.success === false ? "danger" : "success"}>
							{feedback.success === false
								? t("clientDetail.feedback.applyFailed")
								: t("clientDetail.feedback.saved")}
						</Badge>
						{feedback.revision ? (
							<FormDescription style={{ fontSize: 13 }}>
								{t("clientDetail.feedback.desiredRev", {
									n: feedback.revision.desired ?? "—",
								})}
								{" · "}
								{t("clientDetail.feedback.applied", {
									n: feedback.revision.applied ?? "—",
								})}
								{" · "}
								{feedback.revision.state ?? ""}
							</FormDescription>
						) : null}
						{feedback.applyJob?.id ? (
							<FormDescription className="mono" style={{ fontSize: 12 }}>
								{t("clientDetail.feedback.job", {
									id: feedback.applyJob.id,
									status: feedback.applyJob.status ?? "",
								})}
							</FormDescription>
						) : null}
						<Button
							variant="default"
							style={{ marginLeft: "auto" }}
							onClick={() => setFeedback(null)}
						>
							{t("common.dismiss")}
						</Button>
					</div>
				</div>
			) : null}

			{tab === "overview" ? (
				<div className="card">
					<h2>{t("clientDetail.tab.overview")}</h2>
					<p>
						<strong>{t("common.status")}:</strong> {c.status}
					</p>
					<p>
						<strong>{t("clientDetail.quota")}:</strong>{" "}
						{c.quotaBytes != null
							? fmtBytes(c.quotaBytes)
							: t("clientDetail.unlimited")}
					</p>
					<p>
						<strong>{t("clientDetail.expires")}:</strong>{" "}
						{c.expiresAt
							? new Date(c.expiresAt * 1000).toLocaleString()
							: t("clientDetail.never")}
					</p>

					{isAdmin ? (
						<form
							onSubmit={form.handleSubmit((v) => save.mutate(v))}
							style={{ marginTop: 16, maxWidth: 480 }}
						>
							<h2 style={{ fontSize: 14 }}>{t("common.edit")}</h2>
							<FormItem>
								<Label htmlFor="cd-name">{t("common.name")}</Label>
								<Input id="cd-name" {...form.register("name")} />
								<FormMessage>{form.formState.errors.name?.message}</FormMessage>
							</FormItem>
							<FormItem>
								<Label htmlFor="cd-email">{t("clientDetail.email")}</Label>
								<Input id="cd-email" type="email" {...form.register("email")} />
								<FormMessage>
									{form.formState.errors.email?.message}
								</FormMessage>
							</FormItem>
							<FormItem>
								<Label htmlFor="cd-quota">{t("clientDetail.quotaLabel")}</Label>
								<Input
									id="cd-quota"
									inputMode="numeric"
									{...form.register("quotaBytes")}
								/>
								<FormMessage>
									{form.formState.errors.quotaBytes?.message}
								</FormMessage>
							</FormItem>
							<FormItem>
								<Label htmlFor="cd-exp">{t("clientDetail.expiryDate")}</Label>
								<Input
									id="cd-exp"
									type="date"
									{...form.register("expiresAt")}
								/>
							</FormItem>
							<FormItem>
								<Label htmlFor="cd-notes">{t("clientDetail.notes")}</Label>
								<Textarea id="cd-notes" {...form.register("notes")} />
							</FormItem>
							<Label
								style={{
									display: "flex",
									gap: 8,
									alignItems: "center",
									marginBottom: 12,
								}}
							>
								<input type="checkbox" {...form.register("enabled")} />
								<span>{t("common.enabled")}</span>
							</Label>
							<Button
								type="submit"
								variant="primary"
								disabled={save.isPending || !form.formState.isDirty}
							>
								{save.isPending
									? t("clientDetail.saving")
									: t("clientDetail.saveChanges")}
							</Button>
						</form>
					) : null}
				</div>
			) : null}

			{tab === "access" ? (
				<div className="card">
					<h2>{t("clientDetail.bindingsTitle")}</h2>
					{(c.bindings ?? []).length === 0 ? (
						<p className="muted">{t("clientDetail.noBindings")}</p>
					) : (
						(c.bindings ?? []).map((b) => (
							<div
								key={b.id}
								style={{
									border: "1px solid var(--border)",
									borderRadius: 6,
									padding: 12,
									marginBottom: 8,
								}}
							>
								<div
									style={{
										display: "flex",
										alignItems: "center",
										gap: 12,
										flexWrap: "wrap",
									}}
								>
									<strong>{b.inboundId}</strong>
									<Badge variant={b.enabled ? "success" : "default"}>
										{b.enabled
											? t("common.enabled").toLowerCase()
											: t("common.disabled").toLowerCase()}
									</Badge>
									{b.capability?.protocol ? (
										<FormDescription style={{ fontSize: 12 }}>
											{b.capability.protocol}
										</FormDescription>
									) : null}
									<div style={{ flex: 1 }} />
									{isAdmin ? (
										<>
											<Button
												variant="default"
												disabled={toggleBinding.isPending}
												onClick={() => toggleBinding.mutate(b)}
											>
												{b.enabled ? t("common.disable") : t("common.enable")}
											</Button>
											<Button
												variant="default"
												disabled={rotate.isPending}
												onClick={() => rotate.mutate(b.id)}
											>
												{t("clientDetail.rotateCredential")}
											</Button>
											<Button
												variant="danger"
												disabled={detach.isPending}
												onClick={() => detach.mutate(b.id)}
											>
												{t("clientDetail.detach")}
											</Button>
										</>
									) : null}
								</div>
								{b.credential ? (
									<FormDescription style={{ fontSize: 12, marginTop: 6 }}>
										{t("clientDetail.credential")}{" "}
										{b.credential.configured
											? t("clientDetail.credentialConfigured")
											: t("clientDetail.credentialNotSet")}
										{b.credential.version != null
											? ` · v${b.credential.version}`
											: ""}
									</FormDescription>
								) : null}
								{revealed[b.id] ? (
									<div style={{ marginTop: 8 }}>
										<FormDescription style={{ fontSize: 12 }}>
											{t("clientDetail.newCredential")}
										</FormDescription>
										<code className="mono">{revealed[b.id]}</code>
									</div>
								) : null}
							</div>
						))
					)}

					{isAdmin && attachable.length > 0 ? (
						<div className="h-scroll" style={{ gap: 8, marginTop: 12 }}>
							<Select
								style={{ maxWidth: 240 }}
								value={attachInbound}
								onChange={(e) => setAttachInbound(e.target.value)}
								aria-label={t("clientDetail.attachInboundAria")}
							>
								<option value="">{t("clientDetail.attachInbound")}</option>
								{attachable.map((ib) => (
									<option key={ib.name} value={ib.name}>
										{ib.name} ({ib.protocol})
									</option>
								))}
							</Select>
							<Button
								variant="default"
								disabled={!attachInbound || attach.isPending}
								onClick={() => attach.mutate(attachInbound)}
							>
								{t("clientDetail.attach")}
							</Button>
						</div>
					) : null}
				</div>
			) : null}

			{tab === "subscription" ? (
				<>
					<ClientConnectionLinks clientId={clientId} />
					<SubscriptionTokensPanel clientId={clientId} />
				</>
			) : null}

			{tab === "traffic" ? <ClientTrafficPanel clientId={clientId} /> : null}

			{tab === "audit" ? (
				<div className="card">
					<h2>{t("clientDetail.tab.audit")}</h2>
					{audit.isLoading ? (
						<p className="muted">{t("common.loading")}</p>
					) : audit.isError ? (
						<p className="muted">{t("clientDetail.auditUnavailable")}</p>
					) : auditItems.length === 0 ? (
						<p className="muted">{t("clientDetail.noAuditEntries")}</p>
					) : (
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>{t("clientDetail.auditWhen")}</TableHead>
									<TableHead>{t("clientDetail.auditActor")}</TableHead>
									<TableHead>{t("clientDetail.auditAction")}</TableHead>
									<TableHead>{t("clientDetail.auditResult")}</TableHead>
									<TableHead>{t("common.details")}</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{auditItems.map((a, i) => (
									<TableRow key={a.id ?? i}>
										<TableCell className="muted">
											{a.timestamp
												? new Date(a.timestamp).toLocaleString()
												: "—"}
										</TableCell>
										<TableCell>{a.actor ?? "—"}</TableCell>
										<TableCell>{a.action ?? "—"}</TableCell>
										<TableCell>
											<Badge
												variant={a.success === false ? "danger" : "success"}
											>
												{a.success === false
													? t("clientDetail.auditFailed")
													: t("clientDetail.auditOk")}
											</Badge>
										</TableCell>
										<TableCell className="muted">
											{typeof a.details === "string"
												? a.details
												: a.details
													? JSON.stringify(a.details)
													: ""}
										</TableCell>
									</TableRow>
								))}
							</TableBody>
						</Table>
					)}
				</div>
			) : null}
		</>
	);
}
