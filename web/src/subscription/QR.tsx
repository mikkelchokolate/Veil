import QRCode from "qrcode";
import { useEffect, useRef } from "react";
import { useI18n } from "../i18n/I18nContext";

/** Renders a subscription URL as a QR code (B8). Canvas-based so no inline
 * styles / CDN are needed (B12 CSP). */
export function QR({ value, size = 180 }: { value: string; size?: number }) {
	const { t } = useI18n();
	const ref = useRef<HTMLCanvasElement>(null);

	useEffect(() => {
		if (!ref.current || !value) return;
		void QRCode.toCanvas(ref.current, value, {
			width: size,
			margin: 1,
			color: { dark: "#060d0d", light: "#ffe6cb" },
		}).catch(() => {
			/* non-fatal: URL still shown as text */
		});
	}, [value, size]);

	return (
		<canvas
			ref={ref}
			width={size}
			height={size}
			aria-label={t("subTokens.qrAria")}
		/>
	);
}
