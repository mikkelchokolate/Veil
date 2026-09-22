import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../api/fetcher";
import { Badge } from "../components/ui/badge";
import { useI18n } from "../i18n/I18nContext";

/** GET /api/v1/traffic/{clientId} — the backend reports an explicit telemetry
 * state (internal/api/traffic_routes.go clientTrafficReportState):
 *   healthy     — counters observed, providers fine
 *   stale       — counters observed, but a provider is degraded
 *   pending     — accounting configured, never observed (collectedAt is null)
 *   unsupported — no enabled binding reports counters (collectedAt is null)
 * Zero counters without an observation are NOT "no usage" — the UI must
 * surface the state instead of rendering fake zeros or an epoch timestamp
 * (#724). */
interface ClientTraffic {
	clientId: string;
	uploadBytes: number;
	downloadBytes: number;
	usedBytes: number;
	quotaBytes?: number | null;
	remainingBytes?: number | null;
	depleted: boolean;
	state?: string;
	/** Unix seconds; null when the client has never been observed. */
	collectedAt?: number | null;
}

function fmtBytes(n?: number | null): string {
	if (n == null) return "—";
	if (n === 0) return "0 B";
	const units = ["B", "KiB", "MiB", "GiB", "TiB"];
	let v = n;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[i]}`;
}

/** B9: per-client traffic usage + quota progress in the client detail Traffic
 * tab. Honors the API's telemetry state: unobserved clients get explicit
 * copy, not zero counters and a 1970 timestamp. */
export function ClientTrafficPanel({ clientId }: { clientId: string }) {
	const { t } = useI18n();
	const traffic = useQuery<ClientTraffic>({
		queryKey: ["traffic", clientId],
		queryFn: () => apiFetch(`/api/v1/traffic/${clientId}`),
		refetchInterval: 10000,
	});

	if (traffic.isLoading) {
		return (
			<div className="card">
				<p className="muted">{t("common.loading")}</p>
			</div>
		);
	}
	if (traffic.isError || !traffic.data) {
		return (
			<div className="card">
				<p className="form-error">{t("clientTraffic.unavailable")}</p>
			</div>
		);
	}

	const data = traffic.data;
	const state = typeof data.state === "string" ? data.state : "";
	const stateKey = state ? `traffic.state.${state}` : "";
	const stateLabel = state ? t(stateKey) : "";
	// collectedAt is the only signal that a real observation exists — zero
	// counters alone must not be rendered as healthy usage.
	const observed = data.collectedAt != null;
	const collectedAt =
		data.collectedAt != null ? new Date(data.collectedAt * 1000) : null;
	const pct =
		observed && data.quotaBytes != null && data.quotaBytes > 0
			? Math.min(100, Math.round((data.usedBytes / data.quotaBytes) * 100))
			: null;

	return (
		<div className="card">
			<h2>{t("clientTraffic.title")}</h2>
			{state ? (
				<p>
					<strong>{t("traffic.telemetryState")}:</strong>{" "}
					<Badge variant={state === "healthy" ? "success" : "warning"}>
						{stateLabel === stateKey ? state : stateLabel}
					</Badge>
				</p>
			) : null}
			{observed ? (
				<>
					<p>
						<strong>{t("clientTraffic.upload")}:</strong>{" "}
						{fmtBytes(data.uploadBytes)}
					</p>
					<p>
						<strong>{t("clientTraffic.download")}:</strong>{" "}
						{fmtBytes(data.downloadBytes)}
					</p>
					<p>
						<strong>{t("clientTraffic.totalUsed")}:</strong>{" "}
						{fmtBytes(data.usedBytes)}
					</p>
					{data.quotaBytes != null ? (
						<>
							<p>
								<strong>{t("clientTraffic.quota")}:</strong>{" "}
								{fmtBytes(data.quotaBytes)}
								{data.remainingBytes != null
									? ` · ${t("clientTraffic.remaining", { n: fmtBytes(data.remainingBytes) })}`
									: ""}
							</p>
							<progress
								className={`meter-bar${data.depleted ? " is-danger" : ""}`}
								max={100}
								value={pct ?? 0}
							/>
							{data.depleted ? (
								<p className="badge badge-danger" style={{ marginTop: 8 }}>
									{t("clientTraffic.depleted")}
								</p>
							) : null}
						</>
					) : (
						<p className="muted">{t("clientTraffic.noQuota")}</p>
					)}
					<p className="muted" style={{ fontSize: 12, marginTop: 8 }}>
						{t("clientTraffic.collected", {
							at: collectedAt?.toLocaleTimeString() ?? "",
						})}
					</p>
				</>
			) : (
				<p className="muted">
					{state === "unsupported"
						? t("clientTraffic.unsupported")
						: t("clientTraffic.pending")}
				</p>
			)}
		</div>
	);
}
