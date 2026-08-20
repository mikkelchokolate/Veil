import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import type { VersionResponse } from "../api/generated/models";
import {
	PanelRestartTimeoutError,
	postPanelUpdate,
	reloadPanel,
	waitForPanelVersion,
} from "../api/panelUpdate";
import { useApplyState } from "../apply/ApplyStatusIndicator";
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

type UpdatePhase = "idle" | "starting" | "waiting" | "slow";

function PanelVersionCard() {
	const { t } = useI18n();
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [confirm, setConfirm] = useState(false);
	const [phase, setPhase] = useState<UpdatePhase>("idle");
	const [waitProgress, setWaitProgress] = useState<{
		attempt: number;
		max: number;
	} | null>(null);

	const version = useQuery<VersionResponse>({
		queryKey: ["version"],
		queryFn: () => apiFetch("/api/version"),
	});

	const update = useMutation({
		mutationFn: async () => {
			await postPanelUpdate();
			setPhase("waiting");
			return waitForPanelVersion({
				onAttempt: (attempt, max) => setWaitProgress({ attempt, max }),
			});
		},
		onMutate: () => {
			setPhase("starting");
			setWaitProgress(null);
		},
		onSuccess: (next) => {
			qc.setQueryData(["version"], next);
			reloadPanel();
		},
		onError: (error) => {
			setPhase(error instanceof PanelRestartTimeoutError ? "slow" : "idle");
		},
	});

	const busy = update.isPending;

	return (
		<div className="card">
			<h2>{t("overview.version")}</h2>
			{version.isError ? (
				<FormMessage>
					{version.error instanceof ApiError
						? version.error.message
						: t("overview.versionUnavailable")}
				</FormMessage>
			) : (
				<>
					<p>
						{version.data?.version != null ? (
							<span className="mono" data-testid="panel-version">
								{version.data.version}
							</span>
						) : (
							<span className="muted">{t("common.loading")}</span>
						)}
					</p>
					<p>
						<strong>{t("overview.runtime")}:</strong>{" "}
						{version.data?.runtime != null ? (
							<span className="mono muted">{version.data.runtime}</span>
						) : (
							<span className="muted">—</span>
						)}
					</p>
				</>
			)}
			{isAdmin ? (
				<Button
					variant="primary"
					disabled={busy}
					onClick={() => setConfirm(true)}
				>
					{busy ? t("overview.updating") : t("overview.update")}
				</Button>
			) : null}
			{phase === "starting" ? (
				<p className="muted" role="status" aria-live="polite">
					{t("overview.updateStarting")}
				</p>
			) : null}
			{phase === "waiting" ? (
				<p className="muted" role="status" aria-live="polite">
					{waitProgress
						? t("overview.waitingRestart", waitProgress)
						: t("overview.updateStaged")}
				</p>
			) : null}
			{phase === "slow" ? (
				<p className="muted" role="status">
					{t("overview.restartSlow")}
				</p>
			) : null}
			{update.isError && phase !== "slow" ? (
				<FormMessage>
					{t("overview.updateFailed", {
						details:
							update.error instanceof ApiError
								? update.error.message
								: String(update.error),
					})}
				</FormMessage>
			) : null}
			{isAdmin ? (
				<AlertDialog open={confirm} onOpenChange={setConfirm}>
					<AlertDialogContent>
						<AlertDialogHeader>
							<AlertDialogTitle>
								{t("overview.updateConfirmTitle")}
							</AlertDialogTitle>
							<AlertDialogDescription>
								{t("overview.updateConfirmDescription")}
							</AlertDialogDescription>
						</AlertDialogHeader>
						<AlertDialogFooter>
							<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
							<AlertDialogAction
								disabled={busy}
								onClick={(e) => {
									e.preventDefault();
									setConfirm(false);
									update.mutate();
								}}
							>
								{t("overview.updateConfirmAction")}
							</AlertDialogAction>
						</AlertDialogFooter>
					</AlertDialogContent>
				</AlertDialog>
			) : null}
		</div>
	);
}

/** Overview: at-a-glance apply state, client count, version, system vitals. */
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

			<PanelVersionCard />

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
