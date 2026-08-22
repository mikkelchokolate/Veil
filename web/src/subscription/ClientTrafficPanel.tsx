import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../api/fetcher";
import { useI18n } from "../i18n/I18nContext";

interface ClientTraffic {
	clientId: string;
	uploadBytes: number;
	downloadBytes: number;
	usedBytes: number;
	quotaBytes?: number;
	remainingBytes?: number;
	depleted: boolean;
	collectedAt: number;
}

function fmtBytes(n?: number): string {
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
 * tab. */
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
	const pct =
		data.quotaBytes && data.quotaBytes > 0
			? Math.min(100, Math.round((data.usedBytes / data.quotaBytes) * 100))
			: null;

	return (
		<div className="card">
			<h2>{t("clientTraffic.title")}</h2>
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
					at: new Date(data.collectedAt * 1000).toLocaleTimeString(),
				})}
			</p>
		</div>
	);
}
