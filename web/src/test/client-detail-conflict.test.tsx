import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { dateInputToUnix } from "../lib/localDate";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

function renderClientDetail() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/clients/c1"] }),
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
	return { ...view, qc };
}

describe("ClientDetailPage conflict and expiry", () => {
	it("keeps the unsaved draft after a 409 and retries with the refreshed version", async () => {
		const user = userEvent.setup();
		let version = 1;
		let name = "Original";
		let conflictOnce = true;
		const patches: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{ name: "edge", protocol: "hysteria2", enabled: true },
				]),
			),
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name,
					enabled: true,
					version,
					status: "active",
					bindings: [],
				}),
			),
			http.patch("/api/v1/clients/c1", async ({ request }) => {
				const body = (await request.json()) as Record<string, unknown>;
				patches.push(body);
				if (conflictOnce) {
					conflictOnce = false;
					name = "From other tab";
					version = 2;
					return HttpResponse.json(
						{ error: { message: "version conflict" } },
						{ status: 409 },
					);
				}
				if (body.version !== version) {
					return HttpResponse.json(
						{ error: { message: "version conflict" } },
						{ status: 409 },
					);
				}
				return HttpResponse.json({ success: true, version: version + 1 });
			}),
		);
		renderClientDetail();
		const nameField = await screen.findByLabelText(/^name$/i);
		await user.clear(nameField);
		await user.type(nameField, "Local edit");
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		await waitFor(() => expect(patches).toHaveLength(1));
		expect(patches[0]).toEqual({ version: 1, name: "Local edit" });

		await waitFor(() =>
			expect(
				screen.getByText(/updated elsewhere|version conflict/i),
			).toBeInTheDocument(),
		);
		expect(screen.getByLabelText(/^name$/i)).toHaveValue("Local edit");

		await user.click(screen.getByRole("button", { name: /save changes/i }));
		await waitFor(() => expect(patches).toHaveLength(2));
		expect(patches[1]).toEqual({ version: 2, name: "Local edit" });
	});

	it("does not reset a dirty form when the client query refetches", async () => {
		const user = userEvent.setup();
		let name = "Original";
		let notes = "server notes";
		let gets = 0;
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/v1/clients/c1", () => {
				gets += 1;
				return HttpResponse.json({
					id: "c1",
					name,
					notes,
					enabled: true,
					version: 1,
					status: "active",
					bindings: [],
				});
			}),
		);
		const { qc } = renderClientDetail();
		const nameField = await screen.findByLabelText(/^name$/i);
		expect(nameField).toHaveValue("Original");
		await waitFor(() => expect(gets).toBeGreaterThan(0));
		const getsAfterLoad = gets;
		await user.clear(nameField);
		await user.type(nameField, "Unsaved draft");
		name = "From other tab";
		notes = "other notes";
		await qc.invalidateQueries({ queryKey: ["clients", "c1"] });
		await waitFor(() => expect(gets).toBeGreaterThan(getsAfterLoad));
		expect(screen.getByLabelText(/^name$/i)).toHaveValue("Unsaved draft");
	});

	it("reloads server values when the operator discards a conflicted draft", async () => {
		const user = userEvent.setup();
		let version = 1;
		let name = "Original";
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name,
					enabled: true,
					version,
					status: "active",
					bindings: [],
				}),
			),
			http.patch("/api/v1/clients/c1", async () => {
				name = "From other tab";
				version = 2;
				return HttpResponse.json(
					{ error: { message: "version conflict" } },
					{ status: 409 },
				);
			}),
		);
		renderClientDetail();
		const nameField = await screen.findByLabelText(/^name$/i);
		await user.clear(nameField);
		await user.type(nameField, "Local edit");
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		await waitFor(() =>
			expect(
				screen.getByText(/updated elsewhere|version conflict/i),
			).toBeInTheDocument(),
		);
		expect(nameField).toHaveValue("Local edit");
		await user.click(
			screen.getByRole("button", { name: /reload server values/i }),
		);
		await waitFor(() => expect(nameField).toHaveValue("From other tab"));
	});

	it("saves expiry as the local calendar day, not UTC midnight of the date string", async () => {
		const user = userEvent.setup();
		let patch: Record<string, unknown> | null = null;
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name: "Dated",
					enabled: true,
					version: 1,
					status: "active",
					bindings: [],
				}),
			),
			http.patch("/api/v1/clients/c1", async ({ request }) => {
				patch = (await request.json()) as Record<string, unknown>;
				return HttpResponse.json({ success: true });
			}),
		);
		renderClientDetail();
		const expiry = await screen.findByLabelText(/expiry date/i);
		fireEvent.change(expiry, { target: { value: "2026-08-21" } });
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		await waitFor(() => expect(patch).not.toBeNull());
		expect(patch).toEqual({
			version: 1,
			expiresAt: dateInputToUnix("2026-08-21"),
		});
	});
});
