import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { act, render, screen, waitFor } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

// Render the app inside its REAL generated route tree (blocker W6: the page
// reads typed search via Route.useSearch() from the /clients/ file route with
// its Zod validateSearch — file routes are pre-linked to the app root, so a
// hand-built tree would duplicate __root__) + react-query, against the MSW
// mock API.
function renderClients(initialPath = "/clients") {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: [initialPath] }),
	});
	const view = render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<RouterProvider router={router} />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
	return { ...view, router };
}

function searchRequests(onSearch: (search: string) => void) {
	server.use(
		http.get("/api/v1/clients", ({ request }) => {
			onSearch(new URL(request.url).searchParams.get("search") ?? "");
			return HttpResponse.json({
				items: [],
				total: 0,
				page: 1,
				pageSize: 25,
			});
		}),
	);
}

describe("ClientsPage", () => {
	it("renders clients from the API", async () => {
		server.use(
			http.get("/api/v1/clients", () =>
				HttpResponse.json({
					items: [
						{
							id: "c1",
							name: "Alice",
							status: "active",
							enabled: true,
							createdAt: 1700000000,
							bindingCount: 1,
						},
					],
					total: 1,
					page: 1,
					pageSize: 20,
				}),
			),
		);
		renderClients();
		await waitFor(() => expect(screen.getByText("Alice")).toBeInTheDocument());
	});

	it("maps the depleted status filter to quotaState", async () => {
		let seen = "";
		server.use(
			http.get("/api/v1/clients", ({ request }) => {
				seen = new URL(request.url).search;
				return HttpResponse.json({
					items: [],
					total: 0,
					page: 1,
					pageSize: 20,
				});
			}),
		);
		renderClients("/clients?status=depleted");
		await waitFor(() => expect(seen).toContain("quotaState=depleted"));
		expect(seen).not.toContain("status=depleted");
	});

	it("shows empty state when no clients", async () => {
		server.use(
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 20 }),
			),
		);
		renderClients();
		await waitFor(() =>
			expect(screen.getByText(/no clients/i)).toBeInTheDocument(),
		);
	});

	it("follows a same-route search change instead of writing the previous search back", async () => {
		const seen: string[] = [];
		searchRequests((search) => seen.push(search));
		const { router } = renderClients("/clients?search=alice");
		const input = await screen.findByLabelText(/search clients/i);
		await waitFor(() => expect(input).toHaveValue("alice"));
		await waitFor(() => expect(seen).toContain("alice"));

		await act(async () => {
			await router.navigate({ to: "/clients", search: { search: "bob" } });
		});

		await waitFor(() => expect(input).toHaveValue("bob"));
		await waitFor(() => expect(seen).toContain("bob"));
		await act(async () => {
			await new Promise((resolve) => setTimeout(resolve, 400));
		});
		expect(router.state.location.search).toMatchObject({ search: "bob" });
		expect(seen.at(-1)).toBe("bob");
	});

	it("restores a previous search from history without keeping the later one", async () => {
		const seen: string[] = [];
		searchRequests((search) => seen.push(search));
		const { router } = renderClients("/clients?search=alice");
		const input = await screen.findByLabelText(/search clients/i);
		await waitFor(() => expect(input).toHaveValue("alice"));

		await act(async () => {
			await router.navigate({ to: "/clients", search: { search: "bob" } });
		});
		await waitFor(() => expect(input).toHaveValue("bob"));

		await act(async () => {
			router.history.back();
		});
		await waitFor(() => expect(input).toHaveValue("alice"));
		await waitFor(() => expect(seen.at(-1)).toBe("alice"));
		await act(async () => {
			await new Promise((resolve) => setTimeout(resolve, 400));
		});
		expect(router.state.location.search).toMatchObject({ search: "alice" });
	});

	it("clears the search input when the search param is removed", async () => {
		const seen: string[] = [];
		searchRequests((search) => seen.push(search));
		const { router } = renderClients("/clients?search=alice");
		const input = await screen.findByLabelText(/search clients/i);
		await waitFor(() => expect(input).toHaveValue("alice"));

		await act(async () => {
			await router.navigate({ to: "/clients", search: {} });
		});
		await waitFor(() => expect(input).toHaveValue(""));
		await waitFor(() => expect(seen.at(-1)).toBe(""));
		await act(async () => {
			await new Promise((resolve) => setTimeout(resolve, 400));
		});
		expect(router.state.location.search.search).toBeUndefined();
	});

	it("keeps a committed search when the page changes while typing has not settled", async () => {
		const seen: Array<{ search: string; page: string }> = [];
		server.use(
			http.get("/api/v1/clients", ({ request }) => {
				const url = new URL(request.url);
				seen.push({
					search: url.searchParams.get("search") ?? "",
					page: url.searchParams.get("page") ?? "1",
				});
				return HttpResponse.json({
					items: [
						{
							id: "c1",
							name: "Alice",
							status: "active",
							enabled: true,
							createdAt: 1700000000,
						},
					],
					total: 50,
					page: Number(url.searchParams.get("page") ?? 1),
					pageSize: 25,
				});
			}),
		);
		const { router } = renderClients("/clients?search=alice&page=1");
		const input = await screen.findByLabelText(/search clients/i);
		await waitFor(() => expect(input).toHaveValue("alice"));
		await screen.findByText("Alice");

		await act(async () => {
			await router.navigate({
				to: "/clients",
				search: (prev) => ({ ...prev, page: 2 }),
			});
		});
		await waitFor(() =>
			expect(router.state.location.search).toMatchObject({
				search: "alice",
				page: 2,
			}),
		);
		await act(async () => {
			await new Promise((resolve) => setTimeout(resolve, 400));
		});
		expect(router.state.location.search).toMatchObject({
			search: "alice",
			page: 2,
		});
		expect(input).toHaveValue("alice");
		expect(
			seen.some((entry) => entry.search === "alice" && entry.page === "2"),
		).toBe(true);
	});
});
