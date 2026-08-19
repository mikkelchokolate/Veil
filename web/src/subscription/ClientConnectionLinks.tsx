import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import { useI18n } from "../i18n/I18nContext";
import { RevealLink } from "./RevealLink";

interface ClientLink {
	name: string;
	protocol: string;
	transport: string;
	port: number;
	uri: string;
	config?: string;
}

export function ClientConnectionLinks({ clientId }: { clientId: string }) {
	const { t } = useI18n();
	const [copied, setCopied] = useState<string | null>(null);
	const links = useQuery<{ items?: ClientLink[] } | ClientLink[]>({
		queryKey: ["clients", clientId, "links"],
		queryFn: () => apiFetch(`/api/v1/clients/${clientId}/links`),
	});
	const items: ClientLink[] = Array.isArray(links.data)
		? links.data
		: (links.data?.items ?? []);

	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			setCopied(text);
			setTimeout(() => setCopied(null), 1500);
		} catch {
			/* clipboard unavailable */
		}
	}

	return (
		<div className="card">
			<h2>{t("clientLinks.title")}</h2>
			<p className="muted" style={{ fontSize: 13, marginTop: 0 }}>
				{t("clientLinks.hint")}
			</p>
			{links.isLoading ? (
				<p className="muted">{t("common.loading")}</p>
			) : links.isError ? (
				<p className="form-error">
					{links.error instanceof ApiError
						? links.error.message
						: t("clientLinks.error.load")}
				</p>
			) : items.length === 0 ? (
				<p className="muted">{t("clientLinks.empty")}</p>
			) : (
				<div className="form-stack">
					{items.map((link) => (
						<div
							key={`${link.protocol}-${link.port}-${link.uri}`}
							style={{
								border: "1px solid var(--border)",
								padding: 12,
							}}
						>
							<div className="muted" style={{ fontSize: 12 }}>
								{link.name} · {link.protocol} · {link.transport} · {link.port}
							</div>
							<RevealLink
								value={link.uri}
								copied={copied === link.uri}
								onCopy={(text) => void copy(text)}
								showLabel={t("clientLinks.showLink")}
								hideLabel={t("clientLinks.hideLink")}
								copyLabel={t("clientLinks.copy")}
								copiedLabel={t("clientLinks.copied")}
								urlLabel={t("clientLinks.uriLabel")}
								extra={
									link.config ? (
										<pre
											className="mono"
											style={{
												marginTop: 12,
												fontSize: 12,
												whiteSpace: "pre-wrap",
												wordBreak: "break-word",
											}}
										>
											{link.config}
										</pre>
									) : null
								}
							/>
						</div>
					))}
				</div>
			)}
		</div>
	);
}
