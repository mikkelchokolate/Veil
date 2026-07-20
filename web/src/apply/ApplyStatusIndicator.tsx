import { useQuery } from "@tanstack/react-query";
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
}

export function useApplyState() {
	return useQuery<ApplyState>({
		queryKey: ["apply", "state"],
		queryFn: () => apiFetch<ApplyState>("/api/apply/state"),
		refetchInterval: 5000,
	});
}

const STATE_CLS: Record<string, string> = {
	synced: "badge-success",
	pending: "badge-warning",
	applying: "badge-warning",
	failed: "badge-danger",
	rolling_back: "badge-warning",
	rolled_back: "badge-warning",
	degraded: "badge-danger",
};

/** Global apply-status indicator shown on every authenticated page (B5). */
export function ApplyStatusIndicator() {
	const { t } = useI18n();
	const { data, isError } = useApplyState();

	if (isError) {
		return (
			<span className="badge badge-danger">{t("applyState.unavailable")}</span>
		);
	}
	if (!data) {
		return <span className="badge">{t("applyState.loading")}</span>;
	}

	const label = t(`applyState.${data.state}`);
	const meta = {
		label: label === `applyState.${data.state}` ? data.state : label,
		cls: STATE_CLS[data.state] ?? "",
	};
	const drift = data.desiredRevision !== data.appliedRevision;

	return (
		<span
			className={`badge ${meta.cls}`}
			title={
				drift
					? `desired rev ${data.desiredRevision}, runtime rev ${data.appliedRevision}`
					: `runtime rev ${data.appliedRevision}`
			}
		>
			{meta.label}
			{drift ? ` · rev ${data.appliedRevision}→${data.desiredRevision}` : ""}
		</span>
	);
}
