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

describe("RoutingPage WARP-disabled outbound guard", () => {
	// #711: an enabled rule with outbound "warp" fails the apply planner when
	// WARP is disabled — the SPA must not even POST/PUT it.
	it("blocks saving an enabled warp outbound while WARP is disabled", async () => {
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
		expect(save).toBeDisabled();
		expect(screen.getByText(/fails apply/i)).toBeInTheDocument();
		fireEvent.click(save);
		expect(posts).toEqual([]);
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

	// #721: the banner must not claim warp rules merely "have no effect" —
	// an enabled warp rule is a hard plan error while WARP is off.
	it("says warp rules fail apply instead of claiming no effect", async () => {
		server.use(
			http.get("/api/warp", () => HttpResponse.json({ enabled: false })),
			http.get("/api/routing/rules", () => HttpResponse.json([])),
		);
		renderRouting();
		expect(
			await screen.findByText(/fails the apply plan/i),
		).toBeInTheDocument();
		expect(screen.queryByText(/no effect/i)).not.toBeInTheDocument();
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
