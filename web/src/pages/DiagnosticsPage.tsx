import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { mutationErrorMessage } from "../api/fetcher";
import { getApiAudit } from "../api/generated/audit/audit";
import {
	postApiToolsDnsLookup,
	postApiToolsPing,
	postApiToolsSpeedtest,
} from "../api/generated/diagnostics/diagnostics";
import type {
	AuditRecord,
	FirewallRule,
	LogResult,
	StatusResponse,
} from "../api/generated/models";
import { getApiLogs } from "../api/generated/runtime/runtime";
import { getApiFirewall, getApiStatus } from "../api/generated/status/status";
import { useIsAdmin } from "../auth/AuthContext";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
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

// Diagnostics restores the observability surfaces the SPA dropped: the
// structured audit log, managed-unit service logs, and the connectivity
// tools — all wired to the generated API client. The generated GET hooks are
// mutation-shaped in this orval output, so reads go through the generated
// request functions inside useQuery/useInfiniteQuery instead (#1042).

const AUDIT_PAGE_SIZE = 100;
const MAX_LOG_LINES = 500;
const MAX_PING_COUNT = 10;

/** GET /api/audit — newest-first admin audit history with cursor paging. */
function AuditLogCard() {
	const { t } = useI18n();
	const isAdmin = useIsAdmin();
	const audit = useInfiniteQuery({
		queryKey: ["audit"],
		queryFn: ({ pageParam }) =>
			getApiAudit({
				limit: AUDIT_PAGE_SIZE,
				...(pageParam ? { before: pageParam } : {}),
			}),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (last) => last.nextBefore,
		enabled: isAdmin,
	});
	const items: AuditRecord[] =
		audit.data?.pages.flatMap((page) => page.items) ?? [];

	return (
		<div className="card">
			<h2>{t("diagnostics.audit.title")}</h2>
			{!isAdmin ? (
				<p className="muted">{t("diagnostics.adminRequired")}</p>
			) : audit.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : audit.isError ? (
				<FormMessage>
					{mutationErrorMessage(
						audit.error,
						t("diagnostics.audit.loadFailed"),
						t,
					)}
				</FormMessage>
			) : items.length === 0 ? (
				<p className="muted">{t("diagnostics.audit.empty")}</p>
			) : (
				<>
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>{t("diagnostics.audit.time")}</TableHead>
								<TableHead>{t("diagnostics.audit.actor")}</TableHead>
								<TableHead>{t("diagnostics.audit.action")}</TableHead>
								<TableHead>{t("diagnostics.audit.target")}</TableHead>
								<TableHead>{t("diagnostics.audit.ip")}</TableHead>
								<TableHead>{t("diagnostics.audit.result")}</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{items.map((record) => (
								<TableRow
									key={
										record.requestId ??
										`${record.timestamp}|${record.actor}|${record.action}|${record.target ?? ""}`
									}
								>
									<TableCell className="muted">
										{new Date(record.timestamp).toLocaleString()}
									</TableCell>
									<TableCell>
										{record.actor}
										{record.role ? (
											<span className="muted"> · {record.role}</span>
										) : null}
									</TableCell>
									<TableCell className="mono">{record.action}</TableCell>
									<TableCell className="muted">
										{record.target ?? "—"}
									</TableCell>
									<TableCell className="muted">{record.ip ?? "—"}</TableCell>
									<TableCell>
										<Badge variant={record.success ? "success" : "danger"}>
											{record.success
												? t("diagnostics.audit.ok")
												: t("diagnostics.audit.failed")}
										</Badge>
										{record.error ? (
											<div className="muted" style={{ fontSize: 12 }}>
												{record.error}
											</div>
										) : null}
									</TableCell>
								</TableRow>
							))}
						</TableBody>
					</Table>
					{audit.hasNextPage ? (
						<Button
							size="sm"
							style={{ marginTop: 8 }}
							disabled={audit.isFetchingNextPage}
							onClick={() => void audit.fetchNextPage()}
						>
							{audit.isFetchingNextPage
								? t("common.loading")
								: t("diagnostics.audit.loadOlder")}
						</Button>
					) : null}
				</>
			)}
		</div>
	);
}

/** GET /api/logs — bounded journald reads for a managed unit (admin). */
function ServiceLogsCard() {
	const { t } = useI18n();
	const isAdmin = useIsAdmin();
	const [unit, setUnit] = useState("veil");
	const [linesInput, setLinesInput] = useState("50");
	const [requested, setRequested] = useState<{
		unit: string;
		lines: number;
	} | null>(null);
	const [inputError, setInputError] = useState<string | null>(null);

	// The managed units come from /api/status so the operator picks a real
	// journald unit instead of guessing names. The card is admin-only — don't
	// fire the request for viewers.
	const status = useQuery<StatusResponse>({
		queryKey: ["status"],
		queryFn: () => getApiStatus(),
		enabled: isAdmin,
	});
	const unitOptions = (status.data?.services ?? [])
		.filter((s) => s.unit)
		.map((s) => ({
			value: s.unit?.replace(/\.service$/, "") ?? "",
			label: `${s.name} (${s.unit})`,
		}))
		.filter((o) => o.value !== "");
	const options =
		unitOptions.length > 0
			? unitOptions
			: [{ value: "veil", label: "veil (veil.service)" }];
	const selectedUnit = options.some((o) => o.value === unit)
		? unit
		: options[0].value;

	const logs = useQuery<LogResult>({
		queryKey: ["logs", requested],
		queryFn: () =>
			getApiLogs(
				requested ? { unit: requested.unit, lines: requested.lines } : {},
			),
		enabled: isAdmin && requested !== null,
	});

	function load() {
		const lines = Number.parseInt(linesInput, 10);
		if (!Number.isSafeInteger(lines) || lines < 1 || lines > MAX_LOG_LINES) {
			setInputError(t("diagnostics.logs.linesInvalid"));
			return;
		}
		setInputError(null);
		setRequested({ unit: selectedUnit, lines });
	}

	return (
		<div className="card">
			<h2>{t("diagnostics.logs.title")}</h2>
			{!isAdmin ? (
				<p className="muted">{t("diagnostics.adminRequired")}</p>
			) : (
				<>
					<div className="creation-dialog-fields" style={{ marginTop: 8 }}>
						<FormItem>
							<Label htmlFor="log-unit">{t("diagnostics.logs.unit")}</Label>
							<Select
								id="log-unit"
								value={selectedUnit}
								onChange={(e) => setUnit(e.target.value)}
							>
								{options.map((o) => (
									<option key={o.value} value={o.value}>
										{o.label}
									</option>
								))}
							</Select>
						</FormItem>
						<FormItem>
							<Label htmlFor="log-lines">{t("diagnostics.logs.lines")}</Label>
							<Input
								id="log-lines"
								inputMode="numeric"
								value={linesInput}
								onChange={(e) => {
									setLinesInput(e.target.value);
									setInputError(null);
								}}
							/>
						</FormItem>
					</div>
					{inputError ? <FormMessage>{inputError}</FormMessage> : null}
					<Button
						variant="primary"
						style={{ marginTop: 8 }}
						disabled={logs.isFetching}
						onClick={load}
					>
						{logs.isFetching
							? t("diagnostics.logs.loading")
							: t("diagnostics.logs.load")}
					</Button>
					{logs.isError ? (
						<FormMessage>
							{mutationErrorMessage(
								logs.error,
								t("diagnostics.logs.failed"),
								t,
							)}
						</FormMessage>
					) : null}
					<pre
						className="mono"
						aria-live="polite"
						style={{
							marginTop: 8,
							padding: 12,
							maxHeight: 400,
							overflow: "auto",
							background: "var(--surface-alt, var(--background))",
							border: "1px solid var(--border)",
							fontSize: 12,
							whiteSpace: "pre-wrap",
						}}
					>
						{logs.data?.output || t("diagnostics.notStarted")}
					</pre>
				</>
			)}
		</div>
	);
}

/** POST /api/tools/dns-lookup — server-side hostname resolution (viewer+). */
function DnsLookupCard() {
	const { t } = useI18n();
	const [hostname, setHostname] = useState("");
	const [inputError, setInputError] = useState<string | null>(null);
	const lookup = useMutation({
		mutationFn: (value: string) => postApiToolsDnsLookup({ hostname: value }),
	});

	function run() {
		const value = hostname.trim();
		if (!value) {
			setInputError(t("diagnostics.dns.required"));
			return;
		}
		setInputError(null);
		lookup.mutate(value);
	}

	const result = lookup.data;
	return (
		<div className="card">
			<h2>{t("diagnostics.dns.title")}</h2>
			<div className="creation-dialog-fields" style={{ marginTop: 8 }}>
				<FormItem>
					<Label htmlFor="dns-hostname">{t("diagnostics.dns.hostname")}</Label>
					<Input
						id="dns-hostname"
						autoComplete="off"
						value={hostname}
						onChange={(e) => {
							setHostname(e.target.value);
							setInputError(null);
						}}
					/>
				</FormItem>
			</div>
			{inputError ? <FormMessage>{inputError}</FormMessage> : null}
			<Button
				variant="primary"
				style={{ marginTop: 8 }}
				disabled={lookup.isPending}
				onClick={run}
			>
				{lookup.isPending ? t("common.loading") : t("diagnostics.dns.run")}
			</Button>
			{lookup.isError ? (
				<FormMessage>
					{mutationErrorMessage(lookup.error, t("diagnostics.dns.failed"), t)}
				</FormMessage>
			) : null}
			{result ? (
				<div style={{ marginTop: 8 }}>
					{result.error ? <FormMessage>{result.error}</FormMessage> : null}
					<p>
						<strong>{t("diagnostics.dns.addresses")}:</strong>{" "}
						{result.addresses.length > 0 ? (
							<span className="mono">{result.addresses.join(", ")}</span>
						) : (
							<span className="muted">{t("diagnostics.dns.none")}</span>
						)}
					</p>
					{result.cname ? (
						<p>
							<strong>{t("diagnostics.dns.cname")}:</strong>{" "}
							<span className="mono">{result.cname}</span>
						</p>
					) : null}
				</div>
			) : null}
		</div>
	);
}

/** POST /api/tools/ping — server-side reachability probe (viewer+). */
function PingCard() {
	const { t } = useI18n();
	const [host, setHost] = useState("");
	const [count, setCount] = useState("3");
	const [inputError, setInputError] = useState<string | null>(null);
	const ping = useMutation({
		mutationFn: (args: { host: string; count: number }) =>
			postApiToolsPing(args),
	});

	function run() {
		const value = host.trim();
		if (!value) {
			setInputError(t("diagnostics.ping.required"));
			return;
		}
		const n = Number.parseInt(count, 10);
		if (!Number.isSafeInteger(n) || n < 1 || n > MAX_PING_COUNT) {
			setInputError(t("diagnostics.ping.countInvalid"));
			return;
		}
		setInputError(null);
		ping.mutate({ host: value, count: n });
	}

	const result = ping.data;
	return (
		<div className="card">
			<h2>{t("diagnostics.ping.title")}</h2>
			<div className="creation-dialog-fields" style={{ marginTop: 8 }}>
				<FormItem>
					<Label htmlFor="ping-host">{t("diagnostics.ping.host")}</Label>
					<Input
						id="ping-host"
						autoComplete="off"
						value={host}
						onChange={(e) => {
							setHost(e.target.value);
							setInputError(null);
						}}
					/>
				</FormItem>
				<FormItem>
					<Label htmlFor="ping-count">{t("diagnostics.ping.count")}</Label>
					<Input
						id="ping-count"
						inputMode="numeric"
						value={count}
						onChange={(e) => {
							setCount(e.target.value);
							setInputError(null);
						}}
					/>
				</FormItem>
			</div>
			{inputError ? <FormMessage>{inputError}</FormMessage> : null}
			<Button
				variant="primary"
				style={{ marginTop: 8 }}
				disabled={ping.isPending}
				onClick={run}
			>
				{ping.isPending ? t("common.loading") : t("diagnostics.ping.run")}
			</Button>
			{ping.isError ? (
				<FormMessage>
					{mutationErrorMessage(ping.error, t("diagnostics.ping.failed"), t)}
				</FormMessage>
			) : null}
			{result ? (
				<div style={{ marginTop: 8 }}>
					{result.error ? <FormMessage>{result.error}</FormMessage> : null}
					<p>
						{t("diagnostics.ping.result", {
							received: result.received,
							transmitted: result.transmitted,
							loss: result.lossPct,
						})}
					</p>
					{result.avgMs != null ? (
						<p className="muted">
							{t("diagnostics.ping.times", {
								min: result.minMs ?? 0,
								avg: result.avgMs,
								max: result.maxMs ?? 0,
								stddev: result.stddevMs ?? 0,
							})}
						</p>
					) : null}
				</div>
			) : null}
		</div>
	);
}

/** POST /api/tools/speedtest — server-side speedtest (viewer+). */
function SpeedtestCard() {
	const { t } = useI18n();
	const speedtest = useMutation({
		mutationFn: () => postApiToolsSpeedtest(),
	});

	const result = speedtest.data;
	return (
		<div className="card">
			<h2>{t("diagnostics.speedtest.title")}</h2>
			<Button
				variant="primary"
				disabled={speedtest.isPending}
				onClick={() => speedtest.mutate()}
			>
				{speedtest.isPending
					? t("diagnostics.speedtest.running")
					: t("diagnostics.speedtest.run")}
			</Button>
			{speedtest.isError ? (
				<FormMessage>
					{mutationErrorMessage(
						speedtest.error,
						t("diagnostics.speedtest.failed"),
						t,
					)}
				</FormMessage>
			) : null}
			{result ? (
				<p style={{ marginTop: 8 }}>
					{t("diagnostics.speedtest.result", {
						ping: result.pingMs,
						download: result.downloadMbps,
						upload: result.uploadMbps,
					})}
				</p>
			) : null}
		</div>
	);
}

/** GET /api/firewall — planned/managed UFW rule view (viewer+). */
function FirewallCard() {
	const { t } = useI18n();
	const firewall = useQuery<FirewallRule[]>({
		queryKey: ["firewall"],
		queryFn: () => getApiFirewall(),
	});

	return (
		<div className="card">
			<h2>{t("diagnostics.firewall.title")}</h2>
			{firewall.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : firewall.isError ? (
				<FormMessage>
					{mutationErrorMessage(
						firewall.error,
						t("diagnostics.firewall.failed"),
						t,
					)}
				</FormMessage>
			) : (firewall.data ?? []).length === 0 ? (
				<p className="muted">{t("diagnostics.firewall.empty")}</p>
			) : (
				<Table>
					<TableHeader>
						<TableRow>
							<TableHead>{t("diagnostics.firewall.port")}</TableHead>
							<TableHead>{t("diagnostics.firewall.protocol")}</TableHead>
							<TableHead>{t("diagnostics.firewall.service")}</TableHead>
						</TableRow>
					</TableHeader>
					<TableBody>
						{(firewall.data ?? []).map((rule) => (
							<TableRow key={`${rule.protocol}-${rule.port}-${rule.service}`}>
								<TableCell className="mono">{rule.port}</TableCell>
								<TableCell className="muted">{rule.protocol}</TableCell>
								<TableCell>{rule.service}</TableCell>
							</TableRow>
						))}
					</TableBody>
				</Table>
			)}
		</div>
	);
}

/** Diagnostics: audit log, service logs, and connectivity tools. */
export function DiagnosticsPage() {
	const { t } = useI18n();
	return (
		<>
			<div className="card">
				<h2 style={{ margin: 0 }}>{t("diagnostics.title")}</h2>
			</div>
			<AuditLogCard />
			<ServiceLogsCard />
			<DnsLookupCard />
			<PingCard />
			<SpeedtestCard />
			<FirewallCard />
		</>
	);
}
