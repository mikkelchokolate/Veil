import { useQuery } from "@tanstack/react-query";
import { BarChart } from "echarts/charts";
import {
	GridComponent,
	LegendComponent,
	TooltipComponent,
} from "echarts/components";
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";
import { useEffect, useRef } from "react";
import { apiFetch } from "../api/fetcher";
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

function chartOption(
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
	const elRef = useRef<HTMLDivElement>(null);
	const chartRef = useRef<echarts.ECharts | null>(null);
	const itemsRef = useRef(items);
	const tRef = useRef(t);
	itemsRef.current = items;
	tRef.current = t;

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
			chart.setOption(chartOption(itemsRef.current, tRef.current), true);
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
		chartRef.current?.setOption(chartOption(items, t), true);
	}, [items, t]);

	return <div ref={elRef} className="traffic-chart" />;
}

/** B9: traffic dashboard with Apache ECharts breakdown. When no runtime feeds
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
