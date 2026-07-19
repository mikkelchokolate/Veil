import {
	createRootRoute,
	createRoute,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { Outlet } from "@tanstack/react-router";
import { ApplyPage } from "./pages/ApplyPage";
import { BackupsPage } from "./pages/BackupsPage";
import { ClientsPage } from "./pages/ClientsPage";
import { InboundsPage } from "./pages/InboundsPage";
import { OverviewPage } from "./pages/OverviewPage";
import { RoutingPage } from "./pages/RoutingPage";
import { SettingsPage } from "./pages/SettingsPage";
import { SystemPage } from "./pages/SystemPage";
import { TrafficPage } from "./pages/TrafficPage";
import { UsersPage } from "./pages/UsersPage";
import { WarpPage } from "./pages/WarpPage";

// The router basepath matches the panel WebBasePath so client-side nav works
// under "/<secret>" too. Read from the <base href> the server rewrites.
function routerBasepath(): string {
	const el = document.querySelector("base");
	const href = el?.getAttribute("href") ?? "/";
	return href.endsWith("/") && href.length > 1 ? href.slice(0, -1) : href;
}

const rootRoute = createRootRoute({ component: Outlet });

const overviewRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/",
	component: OverviewPage,
});
const clientsRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/clients",
	component: ClientsPage,
});
const inboundsRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/inbounds",
	component: InboundsPage,
});
const routingRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/routing",
	component: RoutingPage,
});
const trafficRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/traffic",
	component: TrafficPage,
});
const warpRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/warp",
	component: WarpPage,
});
const systemRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/system",
	component: SystemPage,
});
const backupsRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/backups",
	component: BackupsPage,
});
const usersRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/users",
	component: UsersPage,
});
const settingsRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/settings",
	component: SettingsPage,
});
const applyRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/apply",
	component: ApplyPage,
});

const routeTree = rootRoute.addChildren([
	overviewRoute,
	clientsRoute,
	inboundsRoute,
	routingRoute,
	trafficRoute,
	warpRoute,
	systemRoute,
	backupsRoute,
	usersRoute,
	settingsRoute,
	applyRoute,
]);

const router = createRouter({
	routeTree,
	basepath: routerBasepath(),
	defaultNotFoundComponent: () => (
		<div className="card">
			<h2>Not found</h2>
			<p className="muted">This page does not exist.</p>
		</div>
	),
});

declare module "@tanstack/react-router" {
	interface Register {
		router: typeof router;
	}
}

export function RouterView() {
	return <RouterProvider router={router} />;
}
