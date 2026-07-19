import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../api/fetcher";

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

const STATE_LABEL: Record<string, { label: string; cls: string }> = {
	synced: { label: "Synced", cls: "badge-success" },
	pending: { label: "Pending", cls: "badge-warning" },
	applying: { label: "Applying", cls: "badge-warning" },
	failed: { label: "Apply failed", cls: "badge-danger" },
	rolling_back: { label: "Rolling back", cls: "badge-warning" },
	rolled_back: { label: "Rolled back", cls: "badge-warning" },
	degraded: { label: "Degraded", cls: "badge-danger" },
};

/** Global apply-status indicator shown on every authenticated page (B5). */
export function ApplyStatusIndicator() {
	const { data, isError } = useApplyState();

	if (isError) {
		return <span className="badge badge-danger">apply state unavailable</span>;
	}
	if (!data) {
		return <span className="badge">apply…</span>;
	}

	const meta = STATE_LABEL[data.state] ?? { label: data.state, cls: "" };
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
