import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { RoutingPage } from "../pages/RoutingPage";
import { HttpResponse, http, server } from "./server";

function renderRouting() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<RoutingPage />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

describe("RoutingPage while WARP is disabled", () => {
	// #1416: disabling WARP keeps the rules and the planner treats them as
	// inert, so the SPA must let the operator save (and edit) them.
	it("allows saving an enabled warp outbound while WARP is disabled", async () => {
		const posts: unknown[] = [];
		server.use(
			http.get("/api/warp", () => HttpResponse.json({ enabled: false })),
			http.get("/api/routing/rules", () => HttpResponse.json([])),
			http.post("/api/routing/rules", async ({ request }) => {
				posts.push(await request.json());
				return HttpResponse.json({ success: true });
			}),
		);
		renderRouting();
		fireEvent.click(await screen.findByRole("button", { name: /new rule/i }));
		fireEvent.change(await screen.findByLabelText(/^name$/i), {
			target: { value: "r1" },
		});
		fireEvent.change(screen.getByLabelText(/match/i), {
			target: { value: "geoip:cn" },
		});
		fireEvent.change(screen.getByLabelText(/outbound/i), {
			target: { value: "warp" },
		});
		const save = screen.getByRole("button", { name: /^save$/i });
		expect(save).toBeEnabled();
		expect(
			screen.getByText(/kept but not applied while WARP is disabled/i),
		).toBeInTheDocument();
		fireEvent.click(save);
		await waitFor(() => expect(posts).toHaveLength(1));
	});

	// #711: a *disabled* warp rule is skipped by the apply planner, so the
	// guard must not block it — the contract is about enabled rules only.
	it("still allows saving a disabled warp rule while WARP is off", async () => {
		const posts: unknown[] = [];
		server.use(
			http.get("/api/warp", () => HttpResponse.json({ enabled: false })),
			http.get("/api/routing/rules", () => HttpResponse.json([])),
			http.post("/api/routing/rules", async ({ request }) => {
				posts.push(await request.json());
				return HttpResponse.json({ success: true });
			}),
		);
		renderRouting();
		fireEvent.click(await screen.findByRole("button", { name: /new rule/i }));
		fireEvent.change(await screen.findByLabelText(/^name$/i), {
			target: { value: "r1" },
		});
		fireEvent.change(screen.getByLabelText(/match/i), {
			target: { value: "geoip:cn" },
		});
		fireEvent.change(screen.getByLabelText(/outbound/i), {
			target: { value: "warp" },
		});
		fireEvent.click(screen.getByLabelText(/enabled/i));
		const save = screen.getByRole("button", { name: /^save$/i });
		expect(save).toBeEnabled();
		fireEvent.click(save);
		await waitFor(() => expect(posts).toHaveLength(1));
	});

	// #1416: the banner tells the operator the rules survive the disable and are
	// inert until WARP is re-enabled.
	it("explains that warp rules are kept while WARP is off", async () => {
		server.use(
			http.get("/api/warp", () => HttpResponse.json({ enabled: false })),
			http.get("/api/routing/rules", () => HttpResponse.json([])),
		);
		renderRouting();
		expect(
			await screen.findByText(/kept but not applied until WARP is enabled/i),
		).toBeInTheDocument();
		expect(screen.queryByText(/fails the apply plan/i)).not.toBeInTheDocument();
	});

	it("keeps warp saves working while WARP is enabled", async () => {
		const posts: unknown[] = [];
		server.use(
			http.get("/api/warp", () => HttpResponse.json({ enabled: true })),
			http.get("/api/routing/rules", () => HttpResponse.json([])),
			http.post("/api/routing/rules", async ({ request }) => {
				posts.push(await request.json());
				return HttpResponse.json({ success: true });
			}),
		);
		renderRouting();
		fireEvent.click(await screen.findByRole("button", { name: /new rule/i }));
		fireEvent.change(await screen.findByLabelText(/^name$/i), {
			target: { value: "r1" },
		});
		fireEvent.change(screen.getByLabelText(/match/i), {
			target: { value: "geoip:cn" },
		});
		fireEvent.change(screen.getByLabelText(/outbound/i), {
			target: { value: "warp" },
		});
		const save = screen.getByRole("button", { name: /^save$/i });
		expect(save).toBeEnabled();
		fireEvent.click(save);
		await waitFor(() => expect(posts).toHaveLength(1));
	});
});

// #1416: a rule that survived a WARP disable must stay editable while WARP is
// still off — otherwise the operator is stuck with a hidden rule.
it("lets an existing warp rule be edited while WARP is disabled", async () => {
	const puts: unknown[] = [];
	server.use(
		http.get("/api/warp", () => HttpResponse.json({ enabled: false })),
		http.get("/api/routing/rules", () =>
			HttpResponse.json([
				{
					name: "warp-openai",
					match: "geosite:openai",
					outbound: "warp",
					enabled: true,
				},
			]),
		),
		http.put("/api/routing/rules/:name", async ({ request }) => {
			puts.push(await request.json());
			return HttpResponse.json({ success: true });
		}),
	);
	renderRouting();
	fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));
	fireEvent.change(await screen.findByLabelText(/match/i), {
		target: { value: "geosite:openai,geosite:anthropic" },
	});
	const save = screen.getByRole("button", { name: /^save$/i });
	expect(save).toBeEnabled();
	fireEvent.click(save);
	await waitFor(() => expect(puts).toHaveLength(1));
});
