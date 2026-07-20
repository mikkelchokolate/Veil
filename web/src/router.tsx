import {
	createRootRoute,
	createRoute,
	createRouter,
	Outlet,
	RouterProvider,
} from "@tanstack/react-router";
import { lazy, Suspense } from "react";

// The router basepath matches the panel WebBasePath so client-side nav works
// under "/<secret>" too. Read from the <base href> the server rewrites.
function routerBasepath(): string {
	const el = document.querySelector("base");
	const href = el?.getAttribute("href") ?? "/";
	return href.endsWith("/") && href.length > 1 ? href.slice(0, -1) : href;
}

// Error boundary for route errors (B-router).
function RouteErrorBoundary({ error }: { error: Error }) {
	return (
		<div className="card">
			<h2>Something went wrong</h2>
			<p className="muted">{error.message}</p>
			<button type="button" onClick={() => window.location.reload()}>
				Reload
			</button>
		</div>
	);
}

// Loading fallback for lazy routes.
function RouteLoading() {
	return (
		<div className="card">
			<p className="muted">Loading…</p>
		</div>
	);
}

// Lazy-loaded pages (B-router: code splitting per route).
const OverviewPage = lazy(() =>
	import("./pages/OverviewPage").then((m) => ({ default: m.OverviewPage })),
);
const ClientsPage = lazy(() =>
	import("./pages/ClientsPage").then((m) => ({ default: m.ClientsPage })),
);
const ClientNewPage = lazy(() =>
	import("./pages/ClientNewPage").then((m) => ({ default: m.ClientNewPage })),
);
const ClientDetailPage = lazy(() =>
	import("./pages/ClientDetailPage").then((m) => ({
		default: m.ClientDetailPage,
	})),
);
const InboundsPage = lazy(() =>
	import("./pages/InboundsPage").then((m) => ({ default: m.InboundsPage })),
);
const RoutingPage = lazy(() =>
	import("./pages/RoutingPage").then((m) => ({ default: m.RoutingPage })),
);
const TrafficPage = lazy(() =>
	import("./pages/TrafficPage").then((m) => ({ default: m.TrafficPage })),
);
const WarpPage = lazy(() =>
	import("./pages/WarpPage").then((m) => ({ default: m.WarpPage })),
);
const SystemPage = lazy(() =>
	import("./pages/SystemPage").then((m) => ({ default: m.SystemPage })),
);
const BackupsPage = lazy(() =>
	import("./pages/BackupsPage").then((m) => ({ default: m.BackupsPage })),
);
const UsersPage = lazy(() =>
	import("./pages/UsersPage").then((m) => ({ default: m.UsersPage })),
);
const SettingsPage = lazy(() =>
	import("./pages/SettingsPage").then((m) => ({ default: m.SettingsPage })),
);
const ApplyPage = lazy(() =>
	import("./pages/ApplyPage").then((m) => ({ default: m.ApplyPage })),
);
const ApplyJobDetailPage = lazy(() =>
	import("./pages/ApplyJobDetailPage").then((m) => ({
		default: m.ApplyJobDetailPage,
	})),
);

const rootRoute = createRootRoute({ component: Outlet });

const overviewRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<OverviewPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const clientsRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/clients",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<ClientsPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const clientNewRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/clients/new",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<ClientNewPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const clientDetailRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/clients/$clientId",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<ClientDetailPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const inboundsRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/inbounds",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<InboundsPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const routingRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/routing",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<RoutingPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const trafficRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/traffic",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<TrafficPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const warpRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/warp",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<WarpPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const systemRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/system",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<SystemPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const backupsRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/backups",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<BackupsPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const usersRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/users",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<UsersPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const settingsRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/settings",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<SettingsPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const applyRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/apply",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<ApplyPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});
const applyJobDetailRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/apply/jobs/$jobId",
	component: () => (
		<Suspense fallback={<RouteLoading />}>
			<ApplyJobDetailPage />
		</Suspense>
	),
	errorComponent: RouteErrorBoundary,
});

const routeTree = rootRoute.addChildren([
	overviewRoute,
	clientsRoute,
	clientNewRoute,
	clientDetailRoute,
	inboundsRoute,
	routingRoute,
	trafficRoute,
	warpRoute,
	systemRoute,
	backupsRoute,
	usersRoute,
	settingsRoute,
	applyRoute,
	applyJobDetailRoute,
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
