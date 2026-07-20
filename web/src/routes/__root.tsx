import { createRootRoute, Outlet } from "@tanstack/react-router";
import { AppShell } from "../shell/AppShell";

// Root layout: the authenticated app shell wraps every route. Rendering the
// shell inside the router (not around RouterProvider) gives its navigation
// hooks (useRouterState/Link) a live router store.
export const Route = createRootRoute({
	component: () => (
		<AppShell>
			<Outlet />
		</AppShell>
	),
	notFoundComponent: () => (
		<div className="card">
			<h2>Not found</h2>
			<p className="muted">This page does not exist.</p>
		</div>
	),
});
