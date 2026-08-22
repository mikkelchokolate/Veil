import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

function renderNewClient() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/clients/new"] }),
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

describe("ClientNewPage inbound load errors", () => {
	it("does not treat a failed inbound list as empty", async () => {
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json(
					{ error: { message: "inbounds down" } },
					{ status: 500 },
				),
			),
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 25 }),
			),
		);
		renderNewClient();
		fireEvent.change(await screen.findByLabelText(/^name$/i), {
			target: { value: "alice" },
		});
		fireEvent.click(screen.getByRole("button", { name: /^next$/i }));
		fireEvent.click(screen.getByRole("button", { name: /^next$/i }));
		expect(await screen.findByText(/inbounds down/i)).toBeInTheDocument();
		expect(
			screen.queryByText(/no inbounds available/i),
		).not.toBeInTheDocument();
	});
});
