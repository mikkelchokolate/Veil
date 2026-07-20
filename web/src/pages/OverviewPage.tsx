import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { apiFetch } from "../api/fetcher";
import { useApplyState } from "../apply/ApplyStatusIndicator";
import { Badge } from "../components/ui/badge";
import { useI18n } from "../i18n/I18nContext";

interface SystemStats {
	cpuPercent: number;
	memoryUsedMB: number;
	memoryTotalMB: number;
	uptimeSeconds: number;
}

function fmtUptime(sec?: number): string {
	if (sec == null) return "—";
	const d = Math.floor(sec / 86400);
	const h = Math.floor((sec % 86400) / 3600);
	if (d > 0) return `${d}d ${h}h`;
	return `${h}h`;
}

/** Overview: at-a-glance apply state, client count, system vitals. */
export function OverviewPage() {
	const { t } = useI18n();
	const apply = useApplyState();
	const clients = useQuery<{ total?: number }>({
		queryKey: ["clients", "count"],
		queryFn: () => apiFetch("/api/v1/clients?page=1&pageSize=1"),
	});
	const sys = useQuery<SystemStats>({
		queryKey: ["system"],
		queryFn: () => apiFetch("/api/system"),
		refetchInterval: 10000,
	});

	const drift = apply.data
		? apply.data.desiredRevision !== apply.data.appliedRevision
		: false;

	return (
		<>
			<div className="card">
				<h2>{t("overview.title")}</h2>
				<p>
					<strong>{t("overview.applyState")}:</strong>{" "}
					{apply.data ? (
						<Badge variant={drift ? "warning" : "success"}>
							{apply.data.state}
						</Badge>
					) : (
						<span className="muted">—</span>
					)}
				</p>
				{apply.data ? (
					<p className="muted">
						{t("overview.revision", { n: apply.data.appliedRevision })}
						{drift ? ` → ${apply.data.desiredRevision}` : ""}
					</p>
				) : null}
				<p>
					<strong>{t("overview.clients")}:</strong>{" "}
					{clients.data?.total != null ? (
						clients.data.total
					) : (
						<span className="muted">—</span>
					)}
				</p>
				<p>
					<Link to="/clients">{t("overview.manageClients")}</Link> ·{" "}
					<Link to="/apply">{t("nav.apply")}</Link> ·{" "}
					<Link to="/traffic">{t("nav.traffic")}</Link>
				</p>
			</div>

			{sys.data ? (
				<div className="card">
					<h2>{t("overview.system")}</h2>
					<p>
						<strong>CPU:</strong> {sys.data.cpuPercent.toFixed(1)}%
					</p>
					<p>
						<strong>{t("overview.memory")}:</strong> {sys.data.memoryUsedMB} /{" "}
						{sys.data.memoryTotalMB} MiB
					</p>
					<p>
						<strong>{t("overview.uptime")}:</strong>{" "}
						{fmtUptime(sys.data.uptimeSeconds)}
					</p>
				</div>
			) : null}
		</>
	);
}
