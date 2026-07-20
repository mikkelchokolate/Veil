import type { ReactNode } from "react";

export function PlaceholderCard({
	title,
	children,
}: {
	title: string;
	children?: ReactNode;
}) {
	return (
		<div className="card">
			<h2>{title}</h2>
			{children ?? (
				<p className="muted">This section is being migrated to the new UI.</p>
			)}
		</div>
	);
}
