import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createRootRoute,
	createRoute,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen } from "@testing-library/react";
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

// #712: DELETE /api/inbounds/{name} fails closed with 409 while client
// bindings still reference it — the confirm flow must not promise a
// "detaches its clients" cascade that does not exist.
describe("InboundsPage delete confirm", () => {
	it("blocks the delete and lists the attached clients instead of claiming detach", async () => {
		let deletes = 0;
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
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
			http.get("/api/inbounds/:name/clients", () =>
				HttpResponse.json({
					items: [{ id: "client-b", name: "bound-client" }],
					total: 1,
					page: 1,
					pageSize: 500,
				}),
			),
			http.delete("/api/inbounds/:name", () => {
				deletes += 1;
				return HttpResponse.json({ success: true });
			}),
		);
		renderInbounds();
		fireEvent.click(await screen.findByRole("button", { name: /^delete$/i }));
		expect(
			await screen.findByText(/still has 1 attached client/i),
		).toBeInTheDocument();
		// The blocker names the attached client and offers no destructive action.
		expect(
			screen.getByRole("link", { name: "bound-client" }),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /confirm delete/i }),
		).not.toBeInTheDocument();
		expect(screen.queryByText(/detaches its clients/i)).not.toBeInTheDocument();
		expect(deletes).toBe(0);
	});

	it("describes only the listener removal for an unattached inbound", async () => {
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{
						name: "free",
						protocol: "hysteria2",
						transport: "udp",
						port: 443,
						enabled: true,
					},
				]),
			),
			http.get("/api/protocols", () => HttpResponse.json([])),
			http.get("/api/inbounds/:name/clients", () =>
				HttpResponse.json({
					items: [],
					total: 0,
					page: 1,
					pageSize: 500,
				}),
			),
		);
		renderInbounds();
		fireEvent.click(await screen.findByRole("button", { name: /^delete$/i }));
		expect(
			await screen.findByText(/removes the listener\. this cannot be undone/i),
		).toBeInTheDocument();
		expect(screen.queryByText(/detaches/i)).not.toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /confirm delete/i }),
		).toBeInTheDocument();
	});
});
