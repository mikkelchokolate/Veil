import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { ApplyStatusIndicator } from "../apply/ApplyStatusIndicator";
import { useAuth } from "../auth/AuthContext";
import { useI18n } from "../i18n/I18nContext";
import { NAV_ENTRIES } from "./nav";

export function AppShell({ children }: { children?: ReactNode }) {
	const { session, logout } = useAuth();
	const pathname = useRouterState({ select: (s) => s.location.pathname });
	const { t, locale, setLocale } = useI18n();

	return (
		<div className="app-shell">
			<aside className="sidebar">
				<div className="sidebar-brand">Veil</div>
				<ul className="nav-menu">
					{NAV_ENTRIES.map((entry) => {
						const active =
							entry.to === "/"
								? pathname === "/"
								: pathname.startsWith(entry.to);
						return (
							<li key={entry.to}>
								<Link
									to={entry.to}
									className={`nav-item${active ? " active" : ""}`}
								>
									<entry.icon className="icon" aria-hidden="true" />
									<span>{t(entry.labelKey)}</span>
								</Link>
							</li>
						);
					})}
				</ul>
			</aside>
			<div className="content-wrapper">
				<header className="top-bar">
					<h1>{t("shell.panelTitle")}</h1>
					<div style={{ display: "flex", alignItems: "center", gap: 16 }}>
						<ApplyStatusIndicator />
						<span className="muted" style={{ fontSize: 13 }}>
							{session?.username}
							{session?.role ? ` · ${session.role}` : ""}
						</span>
						<select
							className="input"
							style={{ maxWidth: 80 }}
							value={locale}
							onChange={(e) => setLocale(e.target.value as "en" | "ru")}
						>
							<option value="en">EN</option>
							<option value="ru">RU</option>
						</select>
						<button type="button" className="btn" onClick={() => void logout()}>
							{t("common.logout")}
						</button>
					</div>
				</header>
				<main className="page-content">{children ?? <Outlet />}</main>
			</div>
		</div>
	);
}
