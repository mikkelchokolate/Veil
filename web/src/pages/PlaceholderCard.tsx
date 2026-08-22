import type { ReactNode } from "react";
import { useI18n } from "../i18n/I18nContext";

export function PlaceholderCard({
	title,
	children,
}: {
	title: string;
	children?: ReactNode;
}) {
	const { t } = useI18n();
	return (
		<div className="card">
			<h2>{title}</h2>
			{children ?? <p className="muted">{t("common.placeholder")}</p>}
		</div>
	);
}
