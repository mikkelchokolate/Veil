import { type ReactNode, useState } from "react";
import { QR } from "./QR";

/** URL + QR stay available after reload but stay hidden until the operator
 * asks to see them. */
export function RevealLink({
	value,
	copied,
	onCopy,
	showLabel,
	hideLabel,
	copyLabel,
	copiedLabel,
	urlLabel,
	extra,
}: {
	value: string;
	copied: boolean;
	onCopy: (text: string) => void;
	showLabel: string;
	hideLabel: string;
	copyLabel: string;
	copiedLabel: string;
	urlLabel: string;
	extra?: ReactNode;
}) {
	const [open, setOpen] = useState(false);
	if (!open) {
		return (
			<div style={{ marginTop: 12 }}>
				<button type="button" className="btn" onClick={() => setOpen(true)}>
					{showLabel}
				</button>
			</div>
		);
	}
	return (
		<div
			style={{
				display: "flex",
				gap: 20,
				alignItems: "flex-start",
				flexWrap: "wrap",
				marginTop: 12,
			}}
		>
			<QR value={value} />
			<div style={{ flex: 1, minWidth: 240 }}>
				<div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
					{urlLabel}
				</div>
				<code className="mono" style={{ wordBreak: "break-all", fontSize: 12 }}>
					{value}
				</code>
				<div
					style={{ marginTop: 12, display: "flex", gap: 8, flexWrap: "wrap" }}
				>
					<button type="button" className="btn" onClick={() => onCopy(value)}>
						{copied ? copiedLabel : copyLabel}
					</button>
					<button type="button" className="btn" onClick={() => setOpen(false)}>
						{hideLabel}
					</button>
				</div>
				{extra}
			</div>
		</div>
	);
}
