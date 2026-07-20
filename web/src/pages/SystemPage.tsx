import { useQuery } from "@tanstack/react-query";
import { ApiError, apiFetch } from "../api/fetcher";
import type { SystemStats } from "../api/generated/models";
import { FormMessage } from "../components/ui/form";
import { useI18n } from "../i18n/I18nContext";

function fmtUptime(sec: number): string {
	const d = Math.floor(sec / 86400);
	const h = Math.floor((sec % 86400) / 3600);
	const m = Math.floor((sec % 3600) / 60);
	if (d > 0) return `${d}d ${h}h`;
	if (h > 0) return `${h}h ${m}m`;
	return `${m}m`;
}

function pct(used: number, total: number): number {
	return total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0;
}

function Meter({
	label,
	value,
	detail,
}: {
	label: string;
	value: number;
	detail: string;
}) {
	return (
		<div style={{ marginBottom: 16 }}>
			<div
				style={{
					display: "flex",
					justifyContent: "space-between",
					marginBottom: 4,
				}}
			>
				<strong>{label}</strong>
				<span className="muted">{detail}</span>
			</div>
			<div
				style={{
					height: 8,
					borderRadius: 4,
					border: "1px solid var(--border)",
					overflow: "hidden",
				}}
				role="progressbar"
				aria-valuenow={value}
				aria-valuemin={0}
				aria-valuemax={100}
			>
				<div
					style={{
						height: "100%",
						width: `${value}%`,
						background: value > 85 ? "var(--accent-danger)" : "var(--accent)",
					}}
				/>
			</div>
		</div>
	);
}

/** System overview: live host telemetry (CPU / memory / disk / load / uptime). */
export function SystemPage() {
	const { t } = useI18n();
	const sys = useQuery<SystemStats>({
		queryKey: ["system"],
		queryFn: () => apiFetch("/api/system"),
		refetchInterval: 5000,
	});

	if (sys.isLoading) {
		return (
			<div className="card">
				<p className="muted">{t("common.loading")}</p>
			</div>
		);
	}
	if (sys.isError || !sys.data) {
		return (
			<div className="card">
				<FormMessage>
					{sys.error instanceof ApiError
						? sys.error.message
						: t("system.unavailable")}
				</FormMessage>
			</div>
		);
	}

	const s = sys.data;
	return (
		<div className="card">
			<h2>{t("system.title")}</h2>
			<Meter
				label={t("system.cpu")}
				value={Math.round(s.cpuPercent)}
				detail={`${s.cpuPercent.toFixed(1)}%`}
			/>
			<Meter
				label={t("system.memory")}
				value={pct(s.memoryUsedMB, s.memoryTotalMB)}
				detail={`${s.memoryUsedMB} / ${s.memoryTotalMB} MiB`}
			/>
			<Meter
				label={t("system.disk")}
				value={pct(s.diskUsedGB, s.diskTotalGB)}
				detail={`${s.diskUsedGB.toFixed(1)} / ${s.diskTotalGB.toFixed(1)} GiB`}
			/>
			<p>
				<strong>{t("system.loadAverage")}:</strong> {s.loadAvg1.toFixed(2)} ·{" "}
				{s.loadAvg5.toFixed(2)} · {s.loadAvg15.toFixed(2)}
			</p>
			<p>
				<strong>{t("system.uptime")}:</strong> {fmtUptime(s.uptimeSeconds)}
			</p>
		</div>
	);
}
