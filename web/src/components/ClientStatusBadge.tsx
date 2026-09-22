import { useI18n } from "../i18n/I18nContext";
import { Badge } from "./ui/badge";

/** Severity mapping for the backend's effective client status
 * (internal/client/model.go ComputeStatus). Unknown values fall back to the
 * default variant and the raw enum so a new status never renders blank. */
export const CLIENT_STATUS_VARIANT: Record<
	string,
	"success" | "warning" | "danger" | "default"
> = {
	active: "success",
	disabled: "default",
	expired: "warning",
	depleted: "warning",
	pending_apply: "warning",
	apply_failed: "danger",
	orphaned: "danger",
	unsupported_telemetry: "default",
};

/** Localized effective-status badge — the single mapping shared by the
 * Clients list and the client detail overview (#732). */
export function ClientStatusBadge({ status }: { status: string }) {
	const { t } = useI18n();
	const variant = CLIENT_STATUS_VARIANT[status] ?? "default";
	const key = `clients.status.${status}`;
	const label = t(key) === key ? status : t(key);
	return <Badge variant={variant}>{label}</Badge>;
}
