import { useQuery } from "@tanstack/react-query";
import { BarChart } from "echarts/charts";
import {
	GridComponent,
	LegendComponent,
	TooltipComponent,
} from "echarts/components";
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";
import { useEffect, useMemo, useRef } from "react";
import { apiFetch, mutationErrorMessage } from "../api/fetcher";
import type {
	ConnectionsStats,
	PresenceItem,
	PresenceResponse,
	TrafficBucket,
	TrafficHistoryResponse,
} from "../api/generated/models";
import { Badge } from "../components/ui/badge";
import { FormMessage } from "../components/ui/form";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "../components/ui/table";
import { useI18n } from "../i18n/I18nContext";
import { fmtBytes } from "../lib/bytes";
import { escapeHtml } from "../lib/escapeHtml";

interface TrafficProviderHealth {
	key: string;
	state: string;
	lastError?: string;
}

interface TrafficSummary {
	state: string;
	providerCount?: number;
	providers?: TrafficProviderHealth[];
	uploadBytes?: number;
	downloadBytes?: number;
	usedBytes?: number;
}

type TopEntry = {
	clientId: string;
	name: string;
	uploadBytes?: number;
	downloadBytes?: number;
	totalBytes?: number;
	usedBytes?: number;
};

function talkerTotalBytes(entry: TopEntry): number | undefined {
	if (entry.totalBytes != null && Number.isFinite(entry.totalBytes)) {
		return entry.totalBytes;
	}
	if (entry.usedBytes != null && Number.isFinite(entry.usedBytes)) {
		return entry.usedBytes;
	}
	if (entry.uploadBytes != null || entry.downloadBytes != null) {
		return (entry.uploadBytes ?? 0) + (entry.downloadBytes ?? 0);
	}
	return undefined;
}

echarts.use([
	BarChart,
	GridComponent,
	LegendComponent,
	TooltipComponent,
	CanvasRenderer,
]);

/** Shared echarts host: owns init/resize/dispose so each card only supplies an
 * option object. The mount-time apply reads optionRef so the first paint uses
 * the freshest option even when an earlier resize tick re-runs apply(). */
function EChartView({ option }: { option: echarts.EChartsCoreOption }) {
	const elRef = useRef<HTMLDivElement>(null);
	const chartRef = useRef<echarts.ECharts | null>(null);
	const optionRef = useRef(option);
	optionRef.current = option;

	useEffect(() => {
		const el = elRef.current;
		if (!el) return;
		let disposed = false;

		const apply = () => {
			if (disposed) return;
			let chart = chartRef.current;
			if (!chart) {
				if (el.clientWidth === 0 || el.clientHeight === 0) return;
				chart = echarts.init(el);
				chartRef.current = chart;
			}
			chart.setOption(optionRef.current, true);
		};

		apply();
		const onResize = () => {
			apply();
			chartRef.current?.resize();
		};
		window.addEventListener("resize", onResize);
		const ro =
			typeof ResizeObserver === "undefined"
				? null
				: new ResizeObserver(onResize);
		ro?.observe(el);

		return () => {
			disposed = true;
			window.removeEventListener("resize", onResize);
			ro?.disconnect();
			try {
				chartRef.current?.dispose();
			} catch {
				/* painter may already be torn down */
			}
			chartRef.current = null;
		};
	}, []);

	useEffect(() => {
		chartRef.current?.setOption(option, true);
	}, [option]);

	return <div ref={elRef} className="traffic-chart" />;
}

function topChartOption(
	items: TopEntry[],
	t: (key: string) => string,
): echarts.EChartsCoreOption {
	const uploadLabel = t("traffic.upload");
	const downloadLabel = t("traffic.download");
	const totalLabel = t("traffic.total");
	return {
		tooltip: {
			trigger: "axis",
			axisPointer: { type: "shadow" },
			formatter: (params: unknown) => {
				const p = params as Array<{
					name: string;
					value: number;
					seriesName: string;
				}>;
				if (!p.length) return "";
				// Client names are user-controlled and may contain HTML
				// metacharacters; the tooltip formatter emits raw HTML, so the
				// name must be escaped before interpolation.
				const name = escapeHtml(p[0].name);
				const up = p.find((x) => x.seriesName === uploadLabel)?.value ?? 0;
				const down = p.find((x) => x.seriesName === downloadLabel)?.value ?? 0;
				return `${name}<br/>${uploadLabel}: ${fmtBytes(up)}<br/>${downloadLabel}: ${fmtBytes(down)}<br/>${totalLabel}: ${fmtBytes(up + down)}`;
			},
		},
		legend: { data: [uploadLabel, downloadLabel] },
		grid: { left: "3%", right: "4%", bottom: "3%", containLabel: true },
		xAxis: {
			type: "category",
			data: items.map((entry) => entry.name),
			axisLabel: { rotate: 30 },
		},
		yAxis: {
			type: "value",
			axisLabel: { formatter: (v: number) => fmtBytes(v) },
		},
		series: [
			{
				name: uploadLabel,
				type: "bar",
				stack: "total",
				data: items.map((entry) => entry.uploadBytes ?? 0),
				itemStyle: { color: "#3b82f6" },
			},
			{
				name: downloadLabel,
				type: "bar",
				stack: "total",
				data: items.map((entry) => entry.downloadBytes ?? 0),
				itemStyle: { color: "#10b981" },
			},
		],
	};
}

function TrafficUsageChart({ items }: { items: TopEntry[] }) {
	const { t } = useI18n();
	const option = useMemo(() => topChartOption(items, t), [items, t]);
	return <EChartView option={option} />;
}

function fmtBucketTime(bucketStart: number): string {
	return new Date(bucketStart * 1000).toLocaleTimeString(undefined, {
		hour: "2-digit",
		minute: "2-digit",
	});
}

function historyChartOption(
	buckets: TrafficBucket[],
	t: (key: string) => string,
): echarts.EChartsCoreOption {
	const uploadLabel = t("traffic.upload");
	const downloadLabel = t("traffic.download");
	return {
		tooltip: {
			trigger: "axis",
			axisPointer: { type: "shadow" },
			formatter: (params: unknown) => {
				const p = params as Array<{
					name: string;
					value: number;
					seriesName: string;
				}>;
				if (!p.length) return "";
				// Axis labels come from Date formatting, but the tooltip emits raw
				// HTML — escape anyway so a hostile locale string cannot inject.
				const name = escapeHtml(p[0].name);
				const up = p.find((x) => x.seriesName === uploadLabel)?.value ?? 0;
				const down = p.find((x) => x.seriesName === downloadLabel)?.value ?? 0;
				return `${name}<br/>${uploadLabel}: ${fmtBytes(up)}<br/>${downloadLabel}: ${fmtBytes(down)}`;
			},
		},
		legend: { data: [uploadLabel, downloadLabel] },
		grid: { left: "3%", right: "4%", bottom: "3%", containLabel: true },
		xAxis: {
			type: "category",
			data: buckets.map((b) => fmtBucketTime(b.bucketStart)),
		},
		yAxis: {
			type: "value",
			axisLabel: { formatter: (v: number) => fmtBytes(v) },
		},
		series: [
			{
				name: uploadLabel,
				type: "bar",
				stack: "total",
				data: buckets.map((b) => b.uploadDelta),
				itemStyle: { color: "#3b82f6" },
			},
			{
				name: downloadLabel,
				type: "bar",
				stack: "total",
				data: buckets.map((b) => b.downloadDelta),
				itemStyle: { color: "#10b981" },
			},
		],
	};
}

const HISTORY_LIMIT = 60;

/** Aggregate bucketed traffic history (GET /api/v1/traffic/history). Buckets
 * carry per-bucket deltas, not cumulative totals — the chart stacks them. */
function HistoryCard() {
	const { t } = useI18n();
	const history = useQuery<TrafficHistoryResponse>({
		queryKey: ["traffic", "history"],
		queryFn: () => apiFetch(`/api/v1/traffic/history?limit=${HISTORY_LIMIT}`),
		refetchInterval: 30000,
	});

	// items is nullable and not guaranteed to arrive sorted — order by bucket
	// start so the chart is always time-ascending.
	const buckets = useMemo(
		() =>
			[...(history.data?.items ?? [])].sort(
				(a, b) => a.bucketStart - b.bucketStart,
			),
		[history.data],
	);
	const option = useMemo(() => historyChartOption(buckets, t), [buckets, t]);

	return (
		<div className="card">
			<h2>{t("traffic.history.title")}</h2>
			{history.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : history.isError ? (
				<FormMessage>
					{mutationErrorMessage(
						history.error,
						t("traffic.history.unavailable"),
						t,
					)}
				</FormMessage>
			) : buckets.length === 0 ? (
				<p className="muted">{t("traffic.history.empty")}</p>
			) : (
				<EChartView option={option} />
			)}
		</div>
	);
}

function PresenceStatusCell({ item }: { item: PresenceItem }) {
	const { t } = useI18n();
	// online is tri-state: true = online, false = offline, null = no telemetry
	// source can prove either. null must never render as a fake "offline".
	if (item.online === true) {
		return <Badge variant="success">{t("traffic.presence.online")}</Badge>;
	}
	if (item.online === false) {
		return <Badge variant="outline">{t("traffic.presence.offline")}</Badge>;
	}
	if (item.source === "ineligible") {
		return <span className="muted">{t("traffic.presence.ineligible")}</span>;
	}
	return <span className="muted">{t("traffic.presence.noTelemetry")}</span>;
}

function PresenceSourceCell({ item }: { item: PresenceItem }) {
	const { t } = useI18n();
	const key = `traffic.presence.source.${item.source}`;
	const label = t(key);
	return <span className="muted">{label === key ? item.source : label}</span>;
}

/** Per-client online presence fed by GET /api/v1/presence (5s poll). */
function PresenceCard() {
	const { t } = useI18n();
	const presence = useQuery<PresenceResponse>({
		queryKey: ["traffic", "presence"],
		queryFn: () => apiFetch("/api/v1/presence"),
		refetchInterval: 5000,
	});

	// API already sorts by clientId; sort anyway so a backend reorder can
	// never reshuffle the table between 5s polls.
	const items = [...(presence.data?.items ?? [])].sort((a, b) =>
		a.clientId.localeCompare(b.clientId),
	);

	return (
		<div className="card">
			<h2>{t("traffic.presence.title")}</h2>
			{presence.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : presence.isError ? (
				<FormMessage>
					{mutationErrorMessage(
						presence.error,
						t("traffic.presence.unavailable"),
						t,
					)}
				</FormMessage>
			) : items.length === 0 ? (
				<p className="muted">{t("traffic.presence.empty")}</p>
			) : (
				<Table>
					<TableHeader>
						<TableRow>
							<TableHead>{t("traffic.client")}</TableHead>
							<TableHead>{t("traffic.presence.status")}</TableHead>
							<TableHead>{t("traffic.presence.source")}</TableHead>
							<TableHead>{t("traffic.presence.connections")}</TableHead>
							<TableHead>{t("traffic.presence.lastActive")}</TableHead>
						</TableRow>
					</TableHeader>
					<TableBody>
						{items.map((item) => (
							<TableRow key={item.clientId}>
								<TableCell>{item.name}</TableCell>
								<TableCell>
									<PresenceStatusCell item={item} />
								</TableCell>
								<TableCell>
									<PresenceSourceCell item={item} />
								</TableCell>
								<TableCell className="mono">
									{item.connections ?? "—"}
								</TableCell>
								<TableCell className="muted">
									{item.lastActiveAt != null
										? new Date(item.lastActiveAt * 1000).toLocaleString()
										: "—"}
								</TableCell>
							</TableRow>
						))}
					</TableBody>
				</Table>
			)}
		</div>
	);
}

/** Listening sockets snapshot fed by GET /api/connections (10s poll). */
function ListenersCard() {
	const { t } = useI18n();
	const conn = useQuery<ConnectionsStats>({
		queryKey: ["traffic", "connections"],
		queryFn: () => apiFetch("/api/connections"),
		refetchInterval: 10000,
	});

	// Deterministic order: port ascending, then proto/address tiebreakers —
	// the backend returns raw procfs order which can shuffle between polls.
	const listeners = useMemo(
		() =>
			[...(conn.data?.listeners ?? [])].sort(
				(a, b) =>
					a.port - b.port ||
					a.proto.localeCompare(b.proto) ||
					a.address.localeCompare(b.address),
			),
		[conn.data],
	);

	return (
		<div className="card">
			<h2>{t("traffic.listeners.title")}</h2>
			{conn.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : conn.isError ? (
				<FormMessage>
					{mutationErrorMessage(
						conn.error,
						t("traffic.listeners.unavailable"),
						t,
					)}
				</FormMessage>
			) : listeners.length === 0 ? (
				<p className="muted">{t("traffic.listeners.empty")}</p>
			) : (
				<Table>
					<TableHeader>
						<TableRow>
							<TableHead>{t("traffic.listeners.proto")}</TableHead>
							<TableHead>{t("traffic.listeners.address")}</TableHead>
							<TableHead>{t("traffic.listeners.port")}</TableHead>
							<TableHead>{t("traffic.listeners.process")}</TableHead>
						</TableRow>
					</TableHeader>
					<TableBody>
						{listeners.map((l) => (
							<TableRow key={`${l.proto}|${l.address}|${l.port}`}>
								<TableCell className="mono">{l.proto}</TableCell>
								<TableCell className="mono">{l.address}</TableCell>
								<TableCell className="mono">{l.port}</TableCell>
								<TableCell className="muted">{l.process ?? "—"}</TableCell>
							</TableRow>
						))}
					</TableBody>
				</Table>
			)}
		</div>
	);
}

/** Traffic observability: aggregate telemetry summary, live per-client
 * presence, listening sockets, bucketed history, and the per-client usage
 * breakdown. Each card polls and fails independently. When no runtime feeds
 * counters the panel says so explicitly instead of rendering a fake graph. */
export function TrafficPage() {
	const { t } = useI18n();
	const summary = useQuery<TrafficSummary>({
		queryKey: ["traffic", "summary"],
		queryFn: () => apiFetch("/api/v1/traffic/summary"),
		refetchInterval: 10000,
	});
	const top = useQuery<{ items: TopEntry[] }>({
		queryKey: ["traffic", "top"],
		queryFn: () => apiFetch("/api/v1/traffic/top"),
		refetchInterval: 10000,
		enabled: (summary.data?.providerCount ?? 0) > 0,
	});

	const state = summary.data?.state;
	const stateKey = state ? `traffic.state.${state}` : "";
	const stateLabel = state ? t(stateKey) : "";
	const hasTelemetry = (summary.data?.providerCount ?? 0) > 0;
	const degradedProviders = (summary.data?.providers ?? []).filter(
		(provider) => provider.state === "degraded",
	);

	return (
		<>
			<div className="card">
				<h2>{t("traffic.title")}</h2>
				{summary.isLoading ? (
					<p className="muted">{t("common.loading")}</p>
				) : summary.isError ? (
					<FormMessage>{t("traffic.summaryUnavailable")}</FormMessage>
				) : summary.data ? (
					<>
						<p>
							<strong>{t("traffic.telemetryState")}:</strong>{" "}
							<Badge variant={state === "healthy" ? "success" : "warning"}>
								{stateLabel === stateKey ? state : stateLabel}
							</Badge>
						</p>
						{degradedProviders.map((provider) => (
							<FormMessage key={provider.key}>
								{t("traffic.providerError", {
									provider: provider.key,
									details: provider.lastError ?? provider.state,
								})}
							</FormMessage>
						))}
						{!hasTelemetry ? (
							<p className="muted">{t("traffic.noTrafficSource")}</p>
						) : (
							<>
								<p>
									<strong>{t("traffic.totalUpload")}:</strong>{" "}
									{fmtBytes(summary.data.uploadBytes)}
								</p>
								<p>
									<strong>{t("traffic.totalDownload")}:</strong>{" "}
									{fmtBytes(summary.data.downloadBytes)}
								</p>
								<p>
									<strong>{t("traffic.totalUsed")}:</strong>{" "}
									{fmtBytes(summary.data.usedBytes)}
								</p>
							</>
						)}
					</>
				) : null}
			</div>

			<PresenceCard />

			<ListenersCard />

			<HistoryCard />

			{hasTelemetry ? (
				<div className="card">
					<h2>{t("traffic.usageBreakdown")}</h2>
					{top.isLoading ? (
						<p className="muted">{t("common.loading")}</p>
					) : top.isError ? (
						<FormMessage>{t("traffic.topUnavailable")}</FormMessage>
					) : (top.data?.items ?? []).length === 0 ? (
						<p className="muted">{t("traffic.noUsageRecorded")}</p>
					) : (
						<>
							<TrafficUsageChart items={top.data?.items ?? []} />
							<div style={{ marginTop: 16 }}>
								<Table>
									<TableHeader>
										<TableRow>
											<TableHead>{t("traffic.client")}</TableHead>
											<TableHead>{t("traffic.upload")}</TableHead>
											<TableHead>{t("traffic.download")}</TableHead>
											<TableHead>{t("traffic.total")}</TableHead>
										</TableRow>
									</TableHeader>
									<TableBody>
										{(top.data?.items ?? []).map((t) => (
											<TableRow key={t.clientId}>
												<TableCell>{t.name}</TableCell>
												<TableCell className="muted">
													{fmtBytes(t.uploadBytes)}
												</TableCell>
												<TableCell className="muted">
													{fmtBytes(t.downloadBytes)}
												</TableCell>
												<TableCell className="muted">
													{fmtBytes(talkerTotalBytes(t))}
												</TableCell>
											</TableRow>
										))}
									</TableBody>
								</Table>
							</div>
						</>
					)}
				</div>
			) : null}
		</>
	);
}
