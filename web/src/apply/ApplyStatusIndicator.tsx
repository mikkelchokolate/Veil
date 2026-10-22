import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { apiFetch } from "../api/fetcher";
import { useI18n } from "../i18n/I18nContext";

export interface ApplyErrorView {
	code?: string;
	message?: string;
}

export interface ApplyState {
	desiredRevision: number;
	appliedRevision: number;
	state: string;
	activeJobId?: string;
	lastSuccessfulJobId?: string;
	lastFailedJobId?: string;
	lastError?: ApplyErrorView;
	progress?: {
		stage: string;
		runtime?: string;
		startedAt: number;
		updatedAt: number;
	};
}

export function useApplyState() {
	return useQuery<ApplyState>({
		queryKey: ["apply", "state"],
		queryFn: () => apiFetch<ApplyState>("/api/apply/state"),
		refetchInterval: (query) => {
			if (query.state.data?.progress) return 1000;
			const state = query.state.data?.state;
			if (
				state === "pending" ||
				state === "applying" ||
				state === "rolling_back" ||
				state === "recovering"
			) {
				return 1000;
			}
			return 5000;
		},
	});
}

export type ApplyStateBadgeVariant =
	| "success"
	| "warning"
	| "danger"
	| "default";

/** Canonical system-state → badge severity map (#691). Shared by the shell
 * indicator and the Overview badge so revision-equal-but-degraded, or
 * untracked-without-tracking, states can never render green on one surface
 * while the other reports danger/warning. */
export const APPLY_STATE_BADGE_VARIANT: Record<string, ApplyStateBadgeVariant> =
	{
		synced: "success",
		pending: "warning",
		applying: "warning",
		failed: "danger",
		rolling_back: "warning",
		rolled_back: "warning",
		degraded: "danger",
		// recovering = a recovery_pending job is active; untracked = no durable
		// apply tracking, so "synced" cannot be proven. Neither may render green.
		recovering: "danger",
		untracked: "warning",
	};

export function applyStateBadgeVariant(state: string): ApplyStateBadgeVariant {
	return APPLY_STATE_BADGE_VARIANT[state] ?? "default";
}

/** Localized label for a system state; an unknown enum still renders the raw
 * value rather than a bare i18n key or a misleading translation. */
export function applyStateLabel(
	t: (key: string) => string,
	state: string,
): string {
	const key = `applyState.${state}`;
	const label = t(key);
	return label === key ? state : label;
}

/** Global apply-status indicator shown on every authenticated page (B5). */
export function ApplyStatusIndicator() {
	const { t } = useI18n();
	const { data, isError } = useApplyState();
	const [now, setNow] = useState(Date.now);
	const hasProgress = !!data?.progress;
	useEffect(() => {
		if (!hasProgress) return;
		setNow(Date.now());
		const timer = globalThis.setInterval(() => setNow(Date.now()), 1000);
		return () => globalThis.clearInterval(timer);
	}, [hasProgress]);

	if (isError) {
		return (
			<span className="badge badge-danger">{t("applyState.unavailable")}</span>
		);
	}
	if (!data) {
		return <span className="badge">{t("applyState.loading")}</span>;
	}

	const variant = applyStateBadgeVariant(data.state);
	const drift = data.desiredRevision !== data.appliedRevision;
	const progress = data.progress;
	const stageKey = `applyProgress.${progress?.stage}`;
	const stageLabel = t(stageKey);
	const elapsed = progress
		? Math.max(0, Math.floor(now / 1000 - progress.startedAt))
		: 0;
	const updatedAgo = progress
		? Math.max(0, Math.floor(now / 1000 - progress.updatedAt))
		: 0;

	return (
		<div className="apply-status-indicator">
			<span
				className={`badge${variant === "default" ? "" : ` badge-${variant}`}`}
				title={
					drift
						? t("applyState.revTooltipDrift", {
								desired: data.desiredRevision,
								applied: data.appliedRevision,
							})
						: t("applyState.revTooltip", { applied: data.appliedRevision })
				}
			>
				{applyStateLabel(t, data.state)}
				{drift
					? t("applyState.revDrift", {
							applied: data.appliedRevision,
							desired: data.desiredRevision,
						})
					: ""}
			</span>
			{progress ? (
				<div className="apply-progress">
					<div role="status">
						{stageLabel === stageKey ? t("applyProgress.working") : stageLabel}
						{progress.runtime ? ` · ${progress.runtime}` : ""}
					</div>
					<div className="muted">
						{t("applyProgress.elapsed", {
							time: `${Math.floor(elapsed / 60)}:${String(elapsed % 60).padStart(2, "0")}`,
						})}
						{" · "}
						{t("applyProgress.updated", { seconds: updatedAgo })}
					</div>
				</div>
			) : null}
		</div>
	);
}
