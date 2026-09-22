import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

function renderClientDetail(locale?: "en" | "ru") {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/clients/c1"] }),
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

function clientApi(client: Record<string, unknown>) {
	server.use(
		http.get("/api/v1/clients/c1", () => HttpResponse.json(client)),
		http.get("/api/inbounds", () => HttpResponse.json([])),
	);
}

describe("ClientDetailPage status badge (#732)", () => {
	it("renders the localized effective status instead of the raw enum (ru)", async () => {
		clientApi({
			id: "c1",
			name: "Alice",
			enabled: true,
			version: 1,
			status: "apply_failed",
			bindings: [],
		});
		renderClientDetail("ru");
		await screen.findByRole("heading", { name: "Alice" });
		expect(await screen.findByText("ошибка применения")).toBeInTheDocument();
		expect(screen.queryByText("apply_failed")).not.toBeInTheDocument();
	});

	it("renders the localized effective status (en)", async () => {
		clientApi({
			id: "c1",
			name: "Alice",
			enabled: true,
			version: 1,
			status: "pending_apply",
			bindings: [],
		});
		renderClientDetail();
		await screen.findByRole("heading", { name: "Alice" });
		expect(await screen.findByText("pending apply")).toBeInTheDocument();
	});
});

describe("inbound pickers expose disabled state (#729)", () => {
	it("marks disabled inbounds in the client detail attach select", async () => {
		server.use(
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name: "Alice",
					enabled: true,
					version: 1,
					status: "active",
					bindings: [],
				}),
			),
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{ name: "edge", protocol: "naiveproxy", enabled: false },
					{ name: "core", protocol: "vless", enabled: true },
				]),
			),
		);
		const user = userEvent.setup();
		renderClientDetail();
		await screen.findByRole("heading", { name: "Alice" });
		await user.click(await screen.findByRole("tab", { name: /^access$/i }));
		const select = await screen.findByLabelText(/attach inbound/i);
		const texts = Array.from(
			select.querySelectorAll("option"),
			(o) => o.textContent,
		);
		expect(texts.some((txt) => /edge.*disabled/i.test(txt ?? ""))).toBe(true);
		expect(texts.some((txt) => /core/i.test(txt ?? ""))).toBe(true);
		expect(texts.some((txt) => /core.*disabled/i.test(txt ?? ""))).toBe(false);
	});

	it("marks disabled inbounds in the new-client access picker", async () => {
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{ name: "edge", protocol: "naiveproxy", enabled: false },
				]),
			),
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 25 }),
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		const router = createRouter({
			routeTree,
			history: createMemoryHistory({ initialEntries: ["/clients/new"] }),
		});
		render(
			<QueryClientProvider client={qc}>
				<AuthProvider>
					<I18nProvider>
						<RouterProvider router={router} />
					</I18nProvider>
				</AuthProvider>
			</QueryClientProvider>,
		);
		fireEvent.change(await screen.findByLabelText(/^name$/i), {
			target: { value: "alice" },
		});
		fireEvent.click(screen.getByRole("button", { name: /^next$/i }));
		fireEvent.click(screen.getByRole("button", { name: /^next$/i }));
		const row = await screen.findByText("edge");
		expect(row.closest("label")?.textContent).toMatch(/disabled/i);
	});
});
