import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { InboundsPage } from "../pages/InboundsPage";
import { HttpResponse, http, server } from "./server";

// #1043: the create/edit form must enforce the server contract
// (internal/inbounds/inbound_validation.go) client-side — name
// ^[A-Za-z0-9_-]+$, port an integer in [1, 65535] — and block the POST/PUT
// instead of letting malformed values sail to a raw 400 or get silently
// corrupted by parseInt ("12abc" -> 12).

function renderInbounds(locale?: "en" | "ru") {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider {...(locale ? { initialLocale: locale } : {})}>
					<InboundsPage />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

function stubCatalog() {
	server.use(
		http.get("/api/inbounds", () => HttpResponse.json([])),
		http.get("/api/protocols", () =>
			HttpResponse.json([
				{
					protocol: "hysteria2",
					displayName: "Hysteria2",
					transports: ["udp"],
				},
			]),
		),
	);
}

async function openCreate() {
	await screen.findByText(/no inbounds configured/i);
	fireEvent.click(screen.getByRole("button", { name: /new inbound/i }));
	await screen.findByLabelText(/^name$/i);
}

describe("InboundsPage client-side validation (#1043)", () => {
	it("blocks an empty name and shows the localized required error", async () => {
		const posts: string[] = [];
		stubCatalog();
		server.use(
			http.post("/api/inbounds", () => {
				posts.push("post");
				return HttpResponse.json({ name: "x", success: true });
			}),
		);
		renderInbounds();
		await openCreate();
		fireEvent.click(screen.getByRole("button", { name: /^create$/i }));
		expect(await screen.findByText("Name is required.")).toBeInTheDocument();
		expect(posts).toEqual([]);
	});

	it("blocks a name outside the server charset and clears the error on edit", async () => {
		const posts: string[] = [];
		stubCatalog();
		server.use(
			http.post("/api/inbounds", () => {
				posts.push("post");
				return HttpResponse.json({ name: "x", success: true });
			}),
		);
		renderInbounds();
		await openCreate();
		const name = screen.getByLabelText(/^name$/i);
		fireEvent.change(name, { target: { value: "bad name!" } });
		fireEvent.click(screen.getByRole("button", { name: /^create$/i }));
		expect(
			await screen.findByText(
				"Name may contain only Latin letters, digits, '-' and '_'.",
			),
		).toBeInTheDocument();
		expect(posts).toEqual([]);
		// Editing the field clears its error before the next submit.
		fireEvent.change(name, { target: { value: "edge-1" } });
		expect(
			screen.queryByText(
				"Name may contain only Latin letters, digits, '-' and '_'.",
			),
		).not.toBeInTheDocument();
	});

	it.each(["", "12abc", "3.14", "0", "65536", "-1"])(
		"blocks invalid port %j without POSTing",
		async (port) => {
			const posts: string[] = [];
			stubCatalog();
			server.use(
				http.post("/api/inbounds", () => {
					posts.push("post");
					return HttpResponse.json({ name: "x", success: true });
				}),
			);
			renderInbounds();
			await openCreate();
			fireEvent.change(screen.getByLabelText(/^name$/i), {
				target: { value: "edge" },
			});
			fireEvent.change(screen.getByLabelText(/^port$/i), {
				target: { value: port },
			});
			fireEvent.click(screen.getByRole("button", { name: /^create$/i }));
			expect(
				await screen.findByText(
					"Port must be a whole number between 1 and 65535.",
				),
			).toBeInTheDocument();
			expect(posts).toEqual([]);
		},
	);

	it("accepts a valid name and port and POSTs once", async () => {
		const posts: Array<Record<string, unknown>> = [];
		stubCatalog();
		server.use(
			http.post("/api/inbounds", async ({ request }) => {
				posts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ name: "edge_1", success: true });
			}),
		);
		renderInbounds();
		await openCreate();
		fireEvent.change(screen.getByLabelText(/^name$/i), {
			target: { value: "edge_1" },
		});
		fireEvent.change(screen.getByLabelText(/^port$/i), {
			target: { value: "65535" },
		});
		fireEvent.click(screen.getByRole("button", { name: /^create$/i }));
		await waitFor(() => expect(posts).toHaveLength(1));
		expect(posts[0]?.name).toBe("edge_1");
		expect(posts[0]?.port).toBe(65535);
	});

	it("localizes the validation errors (ru)", async () => {
		const posts: string[] = [];
		stubCatalog();
		server.use(
			http.post("/api/inbounds", () => {
				posts.push("post");
				return HttpResponse.json({ name: "x", success: true });
			}),
		);
		renderInbounds("ru");
		await screen.findByText(/инбаунды не настроены/i);
		fireEvent.click(screen.getByRole("button", { name: /новый инбаунд/i }));
		fireEvent.click(screen.getByRole("button", { name: /^создать$/i }));
		expect(await screen.findByText("Укажите имя.")).toBeInTheDocument();
		expect(posts).toEqual([]);
	});
});
