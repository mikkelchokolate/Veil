import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { apiFetch, mutationErrorMessage } from "../api/fetcher";
import type { MutationOutcome, WarpConfig } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "../components/ui/alert-dialog";
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

	// Set when the PUT committed but the auto-apply failed (success=false in
	// the mutation envelope); cleared on the next attempt (#643).
	const [applyFailed, setApplyFailed] = useState(false);
	// #707: enable provisions a Cloudflare account server-side and disable
	// can strand warp routing rules — both confirm first.
	const [confirmToggle, setConfirmToggle] = useState(false);

	const toggle = useMutation({
		mutationFn: (enabled: boolean) => {
			const current = warp.data;
			if (!current) throw new Error("warp config not loaded");
			// Echo the GET snapshot, including redacted secrets, so omitted
			// privateKey is not treated as empty and does not re-register.
			return apiFetch<WarpConfig & MutationOutcome>("/api/warp", {
				method: "PUT",
				body: JSON.stringify({ ...current, enabled }),
			});
		},
		onSuccess: (data) => {
			setConfirmToggle(false);
			setApplyFailed(data?.success === false);
			void qc.invalidateQueries({ queryKey: ["warp"] });
			void qc.invalidateQueries({ queryKey: ["apply"] });
		},
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
					{mutationErrorMessage(warp.error, t("warp.unavailable"), t)}
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
					onClick={() => setConfirmToggle(true)}
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
					{mutationErrorMessage(toggle.error, t("warp.toggleFailed"), t)}
				</FormMessage>
			) : null}
			{applyFailed && !toggle.isError ? (
				<FormMessage>{t("warp.applyFailed")}</FormMessage>
			) : null}
			<p className="muted" style={{ fontSize: 12, marginTop: 8 }}>
				{t("warp.provisionNotice")}
			</p>
			<AlertDialog open={confirmToggle} onOpenChange={setConfirmToggle}>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>
							{w.enabled
								? t("warp.disableConfirmTitle")
								: t("warp.enableConfirmTitle")}
						</AlertDialogTitle>
						<AlertDialogDescription>
							{w.enabled
								? t("warp.disableConfirmDescription")
								: t("warp.enableConfirmDescription")}
						</AlertDialogDescription>
					</AlertDialogHeader>
					{toggle.isError ? (
						<FormMessage>
							{mutationErrorMessage(toggle.error, t("warp.toggleFailed"), t)}
						</FormMessage>
					) : null}
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={toggle.isPending}
							onClick={(e) => {
								e.preventDefault();
								toggle.mutate(!w.enabled);
							}}
						>
							{toggle.isPending
								? t("warp.applying")
								: w.enabled
									? t("warp.confirmDisable")
									: t("warp.confirmEnable")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</div>
	);
}
