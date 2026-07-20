import { useQuery } from "@tanstack/react-query";
import * as echarts from "echarts";
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
import { fmtBytes } from "../lib/bytes";

interface TrafficSummary {
	state: string;
	providerCount?: number;
	uploadBytes?: number;
	downloadBytes?: number;
	usedBytes?: number;
}

type TopEntry = TrafficTopEntry;

/** B9: traffic dashboard with Apache ECharts breakdown. When no runtime feeds
 * counters the panel says so explicitly instead of rendering a fake graph. */
export function TrafficPage() {
	const summary = useQuery<TrafficSummary>({
		queryKey: ["traffic", "summary"],
		queryFn: () => apiFetch("/api/v1/traffic/summary"),
		refetchInterval: 10000,
	});
	const top = useQuery<{ items: TopEntry[] }>({
		queryKey: ["traffic", "top"],
		queryFn: () => apiFetch("/api/v1/traffic/top"),
		refetchInterval: 10000,
		enabled: summary.data?.state === "collecting",
	});

	const state = summary.data?.state;
	const chartRef = useRef<HTMLDivElement>(null);
	const chartInstance = useRef<echarts.ECharts | null>(null);

	// Initialize chart when collecting.
	useEffect(() => {
		if (state !== "collecting" || !chartRef.current) return;
		if (!chartInstance.current) {
			chartInstance.current = echarts.init(chartRef.current);
		}
		const items = top.data?.items ?? [];
		const option: echarts.EChartsOption = {
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
					const up = p.find((x) => x.seriesName === "Upload")?.value ?? 0;
					const down = p.find((x) => x.seriesName === "Download")?.value ?? 0;
					return `${name}<br/>Upload: ${fmtBytes(up)}<br/>Download: ${fmtBytes(down)}<br/>Total: ${fmtBytes(up + down)}`;
				},
			},
			legend: { data: ["Upload", "Download"] },
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
					name: "Upload",
					type: "bar",
					stack: "total",
					data: items.map((t) => t.uploadBytes ?? 0),
					itemStyle: { color: "#3b82f6" },
				},
				{
					name: "Download",
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
	}, [state, top.data]);

	// Cleanup on unmount.
	useEffect(() => {
		return () => {
			chartInstance.current?.dispose();
			chartInstance.current = null;
		};
	}, []);

	return (
		<>
			<div className="card">
				<h2>Traffic telemetry</h2>
				{summary.isLoading ? (
					<p className="muted">Loading…</p>
				) : summary.isError ? (
					<FormMessage>Traffic summary unavailable</FormMessage>
				) : summary.data ? (
					<>
						<p>
							<strong>Telemetry state:</strong>{" "}
							<Badge variant={state === "collecting" ? "success" : "warning"}>
								{state}
							</Badge>
						</p>
						{state !== "collecting" ? (
							<p className="muted">
								No traffic source is feeding counters yet. Usage figures below
								are not real telemetry — configure a runtime traffic provider to
								begin collecting.
							</p>
						) : (
							<>
								<p>
									<strong>Total upload:</strong>{" "}
									{fmtBytes(summary.data.uploadBytes)}
								</p>
								<p>
									<strong>Total download:</strong>{" "}
									{fmtBytes(summary.data.downloadBytes)}
								</p>
								<p>
									<strong>Total used:</strong>{" "}
									{fmtBytes(summary.data.usedBytes)}
								</p>
							</>
						)}
					</>
				) : null}
			</div>

			{state === "collecting" ? (
				<div className="card">
					<h2>Usage breakdown</h2>
					{top.isLoading ? (
						<p className="muted">Loading…</p>
					) : (top.data?.items ?? []).length === 0 ? (
						<p className="muted">No usage recorded yet.</p>
					) : (
						<>
							<div ref={chartRef} style={{ width: "100%", height: 320 }} />
							<div style={{ marginTop: 16 }}>
								<Table>
									<TableHeader>
										<TableRow>
											<TableHead>Client</TableHead>
											<TableHead>Upload</TableHead>
											<TableHead>Download</TableHead>
											<TableHead>Total</TableHead>
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
