import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch, mutationErrorMessage } from "../api/fetcher";
import type { WarpConfig } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { FormMessage } from "../components/ui/form";
import { useI18n } from "../i18n/I18nContext";

/** WARP outbound: enable/disable toggle + read-only status. Provisioning of a
 * free Cloudflare account is server-side on enable. */
export function WarpPage() {
	const { t } = useI18n();
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();

	const warp = useQuery<WarpConfig>({
		queryKey: ["warp"],
		queryFn: () => apiFetch("/api/warp"),
	});

	const toggle = useMutation({
		mutationFn: (enabled: boolean) =>
			apiFetch("/api/warp", {
				method: "PUT",
				body: JSON.stringify({ enabled }),
			}),
		onSuccess: () => void qc.invalidateQueries({ queryKey: ["warp"] }),
	});

	if (warp.isLoading) {
		return (
			<div className="card">
				<p className="muted">{t("common.loading")}</p>
			</div>
		);
	}
	if (warp.isError || !warp.data) {
		return (
			<div className="card">
				<FormMessage>
					{mutationErrorMessage(warp.error, t("warp.unavailable"))}
				</FormMessage>
			</div>
		);
	}

	const w = warp.data;
	return (
		<div className="card">
			<h2>{t("warp.title")}</h2>
			<p>
				<strong>{t("warp.status")}:</strong>{" "}
				<Badge variant={w.enabled ? "success" : "default"}>
					{w.enabled ? t("common.enabled") : t("common.disabled")}
				</Badge>
			</p>
			{w.endpoint ? (
				<p>
					<strong>{t("warp.endpoint")}:</strong>{" "}
					<span className="mono muted">{w.endpoint}</span>
				</p>
			) : null}
			{w.localAddress ? (
				<p>
					<strong>{t("warp.localAddress")}:</strong>{" "}
					<span className="mono muted">{w.localAddress}</span>
				</p>
			) : null}
			{w.socksListen ? (
				<p>
					<strong>{t("warp.socks")}:</strong>{" "}
					<span className="mono muted">
						{w.socksListen}
						{w.socksPort ? `:${w.socksPort}` : ""}
					</span>
				</p>
			) : null}
			{w.mtu ? (
				<p>
					<strong>{t("warp.mtu")}:</strong>{" "}
					<span className="muted">{w.mtu}</span>
				</p>
			) : null}
			{isAdmin ? (
				<Button
					variant={w.enabled ? "default" : "primary"}
					disabled={toggle.isPending}
					onClick={() => toggle.mutate(!w.enabled)}
				>
					{toggle.isPending
						? t("warp.applying")
						: w.enabled
							? t("warp.disable")
							: t("warp.enable")}
				</Button>
			) : null}
			{toggle.isError ? (
				<FormMessage>
					{mutationErrorMessage(toggle.error, t("warp.toggleFailed"))}
				</FormMessage>
			) : null}
			<p className="muted" style={{ fontSize: 12, marginTop: 8 }}>
				{t("warp.provisionNotice")}
			</p>
		</div>
	);
}
