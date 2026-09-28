import { useQuery } from "@tanstack/react-query";
import { ApiError, apiFetch, mutationErrorMessage } from "../api/fetcher";
import type { ProcessesStats, SystemStats } from "../api/generated/models";
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
		<div className="meter">
			<div className="meter-head">
				<strong>{label}</strong>
				<span className="muted">{detail}</span>
			</div>
			<progress
				className={`meter-bar${value > 85 ? " is-danger" : ""}`}
				max={100}
				value={value}
			/>
		</div>
	);
}

/** Managed-service process list fed by GET /api/processes (5s poll). */
function ProcessesCard() {
	const { t } = useI18n();
	const proc = useQuery<ProcessesStats>({
		queryKey: ["processes"],
		queryFn: () => apiFetch("/api/processes"),
		refetchInterval: 5000,
	});

	// Deterministic order: name ascending, pid as the tiebreaker.
	const rows = [...(proc.data?.processes ?? [])].sort(
		(a, b) => a.name.localeCompare(b.name) || a.pid - b.pid,
	);

	return (
		<div className="card">
			<h2>{t("system.processes.title")}</h2>
			{proc.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : proc.isError ? (
				<FormMessage>
					{mutationErrorMessage(
						proc.error,
						t("system.processes.unavailable"),
						t,
					)}
				</FormMessage>
			) : rows.length === 0 ? (
				<p className="muted">{t("system.processes.empty")}</p>
			) : (
				<Table>
					<TableHeader>
						<TableRow>
							<TableHead>{t("system.processes.name")}</TableHead>
							<TableHead>{t("system.processes.pid")}</TableHead>
							<TableHead>{t("system.processes.cpu")}</TableHead>
							<TableHead>{t("system.processes.memory")}</TableHead>
							<TableHead>{t("system.processes.uptime")}</TableHead>
						</TableRow>
					</TableHeader>
					<TableBody>
						{rows.map((p) => (
							<TableRow key={p.pid}>
								<TableCell>{p.name}</TableCell>
								<TableCell className="mono">{p.pid}</TableCell>
								<TableCell className="mono">
									{p.cpuPercent.toFixed(1)}%
								</TableCell>
								<TableCell className="mono">{p.memoryMB} MiB</TableCell>
								<TableCell className="muted">
									{fmtUptime(p.uptimeSeconds)}
								</TableCell>
							</TableRow>
						))}
					</TableBody>
				</Table>
			)}
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
		<>
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
			<ProcessesCard />
		</>
	);
}
