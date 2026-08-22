import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

function renderClientDetail() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/clients/c1"] }),
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

describe("ClientDetailPage inbound load errors", () => {
	it("does not hide a failed inbound catalog behind an empty attach list", async () => {
		server.use(
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name: "Alice",
					enabled: true,
					version: 1,
					bindings: [],
				}),
			),
			http.get("/api/inbounds", () =>
				HttpResponse.json(
					{ error: { message: "inbounds down" } },
					{ status: 500 },
				),
			),
		);
		const user = userEvent.setup();
		renderClientDetail();
		await screen.findByRole("heading", { name: "Alice" });
		await user.click(await screen.findByRole("tab", { name: /^access$/i }));
		expect(
			await screen.findByText(/bindings & credentials/i),
		).toBeInTheDocument();
		expect(await screen.findByText(/inbounds down/i)).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /^attach$/i }),
		).not.toBeInTheDocument();
	});
});
