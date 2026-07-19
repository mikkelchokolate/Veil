import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { useAuth } from "../auth/AuthContext";
import { ApplyStatusIndicator } from "../apply/ApplyStatusIndicator";
import { NAV_ENTRIES } from "./nav";

function NavIcon({ path }: { path: string }) {
	return (
		<svg
			className="icon"
			viewBox="0 0 24 24"
			fill="none"
			stroke="currentColor"
			strokeWidth="2"
			strokeLinecap="round"
			strokeLinejoin="round"
			aria-hidden="true"
		>
			<path d={path} />
		</svg>
	);
}

export function AppShell({ children }: { children?: ReactNode }) {
	const { session, logout } = useAuth();
	const pathname = useRouterState({ select: (s) => s.location.pathname });

	return (
		<div className="app-shell">
			<aside className="sidebar">
				<div className="sidebar-brand">Veil</div>
				<ul className="nav-menu">
					{NAV_ENTRIES.map((entry) => {
						const active =
							entry.to === "/" ? pathname === "/" : pathname.startsWith(entry.to);
						return (
							<li key={entry.to}>
								<Link to={entry.to} className={`nav-item${active ? " active" : ""}`}>
									<NavIcon path={entry.icon} />
									<span>{entry.label}</span>
								</Link>
							</li>
						);
					})}
				</ul>
			</aside>
			<div className="content-wrapper">
				<header className="top-bar">
					<h1>Veil Panel</h1>
					<div style={{ display: "flex", alignItems: "center", gap: 16 }}>
						<ApplyStatusIndicator />
						<span className="muted" style={{ fontSize: 13 }}>
							{session?.username}
							{session?.role ? ` · ${session.role}` : ""}
						</span>
						<button type="button" className="btn" onClick={() => void logout()}>
							Logout
						</button>
					</div>
				</header>
				<main className="page-content">{children ?? <Outlet />}</main>
			</div>
		</div>
	);
}
