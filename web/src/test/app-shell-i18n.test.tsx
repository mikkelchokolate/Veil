import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

function renderShell(locale?: "en" | "ru") {
	// The shell renders OverviewPage at "/"; stub its queries so the test
	// only exercises the header.
	server.use(
		http.get("/api/system", () =>
			HttpResponse.json({
				cpuPercent: 0,
				memoryUsedMB: 0,
				memoryTotalMB: 0,
				uptimeSeconds: 0,
			}),
		),
		http.get("/api/version", () => HttpResponse.json({})),
		http.get("/api/v1/clients", () =>
			HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 1 }),
		),
	);
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/"] }),
	});
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider {...(locale ? { initialLocale: locale } : {})}>
					<RouterProvider router={router} />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

describe("AppShell session badge (#708)", () => {
	it("renders the localized role in the shell header (ru)", async () => {
		renderShell("ru");
		// The default /api/auth/status fixture returns role=admin; the header
		// must render the localized label rather than the raw enum value.
		expect(
			await screen.findByText("admin · администратор"),
		).toBeInTheDocument();
	});

	it("renders the localized role (en)", async () => {
		renderShell();
		expect(await screen.findByText("admin · admin")).toBeInTheDocument();
	});
});
