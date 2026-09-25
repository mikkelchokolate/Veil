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

// Regression for #1016: POST /api/v1/clients must carry a stable
// Idempotency-Key. A create that outlives the 60s mutation deadline keeps
// committing server-side; without a key, the operator's retry inserts a
// second same-name client and the first create's one-time credentials are
// unrecoverable.

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

function clientCreated() {
	return HttpResponse.json(
		{
			client: {
				id: "c1",
				name: "alice",
				enabled: true,
				version: 1,
				status: "active",
			},
			success: true,
			revision: { desired: 2, applied: 2, state: "synced" },
		},
		{ status: 201 },
	);
}

function useClientApi(keys: Array<string | null>, failures = 0) {
	let posts = 0;
	server.use(
		http.get("/api/inbounds", () => HttpResponse.json([])),
		http.get("/api/v1/clients", () =>
			HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 25 }),
		),
		http.post("/api/v1/clients", ({ request }) => {
			posts += 1;
			keys.push(request.headers.get("Idempotency-Key"));
			if (posts <= failures) {
				return HttpResponse.json(
					{ error: { code: "internal_error", message: "apply stalled" } },
					{ status: 500 },
				);
			}
			return clientCreated();
		}),
	);
}

async function reachReviewAndCreate(user: ReturnType<typeof userEvent.setup>) {
	await user.type(await screen.findByLabelText(/^name$/i), "alice");
	await user.click(screen.getByRole("button", { name: /^next$/i }));
	await user.click(screen.getByRole("button", { name: /^next$/i }));
	await user.click(screen.getByRole("button", { name: /^review$/i }));
	await user.click(screen.getByRole("button", { name: /create client/i }));
}

describe("ClientNewPage idempotency key", () => {
	it("sends a crypto-random Idempotency-Key on the create POST", async () => {
		const user = userEvent.setup();
		const keys: Array<string | null> = [];
		useClientApi(keys);
		renderNewClient();
		await reachReviewAndCreate(user);
		expect(await screen.findByText(/^client created$/i)).toBeInTheDocument();
		expect(keys).toHaveLength(1);
		expect(keys[0]).toMatch(
			/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
		);
	});

	it("reuses the same Idempotency-Key when the same create is retried", async () => {
		const user = userEvent.setup();
		const keys: Array<string | null> = [];
		useClientApi(keys, 1);
		renderNewClient();
		await reachReviewAndCreate(user);
		// First attempt fails server-side; the create button re-enables and the
		// operator retries the identical request.
		expect(await screen.findByText(/apply stalled/i)).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /create client/i }));
		expect(await screen.findByText(/^client created$/i)).toBeInTheDocument();
		expect(keys).toHaveLength(2);
		expect(keys[0]).toBeTruthy();
		expect(keys[0]).toBe(keys[1]);
	});

	it("mints a fresh Idempotency-Key when the payload changes between attempts", async () => {
		const user = userEvent.setup();
		const keys: Array<string | null> = [];
		useClientApi(keys, 1);
		renderNewClient();
		await reachReviewAndCreate(user);
		expect(await screen.findByText(/apply stalled/i)).toBeInTheDocument();
		// Back to step 0 (review→access→limits→general), change the name, and
		// create again — a deliberately different request must not collide with
		// the first key (409).
		await user.click(screen.getByRole("button", { name: /^back$/i }));
		await user.click(screen.getByRole("button", { name: /^back$/i }));
		await user.click(screen.getByRole("button", { name: /^back$/i }));
		const nameInput = await screen.findByLabelText(/^name$/i);
		await user.clear(nameInput);
		await user.type(nameInput, "bob");
		await user.click(screen.getByRole("button", { name: /^next$/i }));
		await user.click(screen.getByRole("button", { name: /^next$/i }));
		await user.click(screen.getByRole("button", { name: /^review$/i }));
		await user.click(screen.getByRole("button", { name: /create client/i }));
		expect(await screen.findByText(/^client created$/i)).toBeInTheDocument();
		expect(keys).toHaveLength(2);
		expect(keys[0]).toBeTruthy();
		expect(keys[1]).toBeTruthy();
		expect(keys[0]).not.toBe(keys[1]);
	});
});
