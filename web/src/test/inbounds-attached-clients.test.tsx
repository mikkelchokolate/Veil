import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createRootRoute,
	createRoute,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { InboundsPage } from "../pages/InboundsPage";
import { HttpResponse, http, server } from "./server";

function renderInbounds() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const rootRoute = createRootRoute();
	const route = createRoute({
		getParentRoute: () => rootRoute,
		path: "/",
		component: InboundsPage,
	});
	const router = createRouter({
		routeTree: rootRoute.addChildren([route]),
	});
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<RouterProvider router={router} />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

describe("InboundsPage attached clients", () => {
	it("shows a client attached to inbound B even when it is outside a global 500-client page", async () => {
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{
						name: "unrelated",
						protocol: "hysteria2",
						transport: "udp",
						port: 443,
						enabled: true,
					},
					{
						name: "bound",
						protocol: "hysteria2",
						transport: "udp",
						port: 8443,
						enabled: true,
					},
				]),
			),
			http.get("/api/protocols", () =>
				HttpResponse.json([
					{
						protocol: "hysteria2",
						displayName: "Hysteria2",
						transports: ["udp"],
					},
				]),
			),
			http.get("/api/v1/clients", () =>
				HttpResponse.json({
					items: Array.from({ length: 500 }, (_, i) => ({
						id: `other-${i}`,
						name: `other-${i}`,
						inboundIds: ["unrelated"],
					})),
					total: 501,
					page: 1,
					pageSize: 500,
				}),
			),
			http.get("/api/inbounds/:name/clients", ({ params }) => {
				if (params.name === "bound") {
					return HttpResponse.json({
						items: [{ id: "client-b", name: "bound-client" }],
						total: 1,
						page: 1,
						pageSize: 500,
					});
				}
				return HttpResponse.json({
					items: [],
					total: 0,
					page: 1,
					pageSize: 500,
				});
			}),
		);
		renderInbounds();
		expect(await screen.findByText("bound-client")).toBeInTheDocument();
	});
});
