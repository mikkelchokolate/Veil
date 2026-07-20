import { createRootRoute, Outlet } from "@tanstack/react-router";
import { useI18n } from "../i18n/I18nContext";
import { AppShell } from "../shell/AppShell";

// Root layout: the authenticated app shell wraps every route. Rendering the
// shell inside the router (not around RouterProvider) gives its navigation
// hooks (useRouterState/Link) a live router store.
function NotFound() {
	const { t } = useI18n();
	return (
		<div className="card">
			<h2>{t("common.notFound")}</h2>
			<p className="muted">{t("common.notFoundDescription")}</p>
		</div>
	);
}

export const Route = createRootRoute({
	component: () => (
		<AppShell>
			<Outlet />
		</AppShell>
	),
	notFoundComponent: NotFound,
});
