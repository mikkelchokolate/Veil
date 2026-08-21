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
import type { TrafficTopEntry } from "../api/generated/models";
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

type TopEntry = TrafficTopEntry;

echarts.use([
	BarChart,
	GridComponent,
	LegendComponent,
	TooltipComponent,
	CanvasRenderer,
]);

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
	const chartRef = useRef<HTMLDivElement>(null);
	const chartInstance = useRef<echarts.ECharts | null>(null);

	// Initialize chart when collecting. Skip a 0×0 box (jsdom, or CSP
	// hiding the box) — echarts throws if it cannot read clientWidth.
	useEffect(() => {
		if (!hasTelemetry || !chartRef.current) return;
		const el = chartRef.current;
		if (el.clientWidth === 0 || el.clientHeight === 0) return;
		if (!chartInstance.current) {
			chartInstance.current = echarts.init(el);
		}
		const uploadLabel = t("traffic.upload");
		const downloadLabel = t("traffic.download");
		const totalLabel = t("traffic.total");
		const items = top.data?.items ?? [];
		const option: echarts.EChartsCoreOption = {
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
					const name = p[0].name;
					const up = p.find((x) => x.seriesName === uploadLabel)?.value ?? 0;
					const down =
						p.find((x) => x.seriesName === downloadLabel)?.value ?? 0;
					return `${name}<br/>${uploadLabel}: ${fmtBytes(up)}<br/>${downloadLabel}: ${fmtBytes(down)}<br/>${totalLabel}: ${fmtBytes(up + down)}`;
				},
			},
			legend: { data: [uploadLabel, downloadLabel] },
			grid: { left: "3%", right: "4%", bottom: "3%", containLabel: true },
			xAxis: {
				type: "category",
				data: items.map((t) => t.name),
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
					data: items.map((t) => t.uploadBytes ?? 0),
					itemStyle: { color: "#3b82f6" },
				},
				{
					name: downloadLabel,
					type: "bar",
					stack: "total",
					data: items.map((t) => t.downloadBytes ?? 0),
					itemStyle: { color: "#10b981" },
				},
			],
		};
		chartInstance.current.setOption(option);
		const onResize = () => chartInstance.current?.resize();
		window.addEventListener("resize", onResize);
		return () => window.removeEventListener("resize", onResize);
	}, [hasTelemetry, top.data, t]);

	// Cleanup on unmount.
	useEffect(() => {
		return () => {
			try {
				chartInstance.current?.dispose();
			} catch {
				/* painter may already be torn down */
			}
			chartInstance.current = null;
		};
	}, []);

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
					) : (top.data?.items ?? []).length === 0 ? (
						<p className="muted">{t("traffic.noUsageRecorded")}</p>
					) : (
						<>
							<div ref={chartRef} className="traffic-chart" />
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
													{fmtBytes(t.totalBytes)}
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
