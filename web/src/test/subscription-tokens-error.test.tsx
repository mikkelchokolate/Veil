import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import { I18nProvider } from "../i18n/I18nContext";
import { SubscriptionTokensPanel } from "../subscription/SubscriptionTokensPanel";
import { HttpResponse, http, server } from "./server";

vi.mock("../auth/AuthContext", () => ({
	useIsAdmin: () => true,
}));

describe("SubscriptionTokensPanel errors", () => {
	it("does not treat a failed token list as empty", async () => {
		server.use(
			http.get("/api/v1/clients/c1/tokens", () =>
				HttpResponse.json({ error: { message: "forbidden" } }, { status: 403 }),
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<SubscriptionTokensPanel clientId="c1" />
				</I18nProvider>
			</QueryClientProvider>,
		);
		expect(await screen.findByText(/forbidden/i)).toBeInTheDocument();
		expect(
			screen.queryByText(/no subscription tokens/i),
		).not.toBeInTheDocument();
	});

	it("keeps the one-time rotate URL after the token list refetches", async () => {
		const user = userEvent.setup();
		server.use(
			http.get("/api/v1/clients/c1/tokens", () =>
				HttpResponse.json({
					items: [
						{
							id: "tok-1",
							prefix: "veil_ab",
							label: "phone",
							enabled: true,
							createdAt: 1700000000,
						},
					],
				}),
			),
			http.post("/api/v1/clients/c1/tokens/tok-1/rotate", () =>
				HttpResponse.json({
					token: {
						id: "tok-1",
						prefix: "veil_cd",
						label: "phone",
						enabled: true,
						createdAt: 1700000000,
					},
					plaintext: "veil_cd_secret",
					url: "/s/veil_cd_secret",
				}),
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<SubscriptionTokensPanel clientId="c1" />
				</I18nProvider>
			</QueryClientProvider>,
		);
		await user.click(await screen.findByRole("button", { name: /^rotate$/i }));
		expect(
			await screen.findByTestId("issued-subscription-token"),
		).toBeInTheDocument();
		expect(screen.getByText(/new token \(shown once\)/i)).toBeInTheDocument();
	});

	it("shows an expired token as expired and requires a future expiry to rotate", async () => {
		const user = userEvent.setup();
		const rotateBodies: unknown[] = [];
		const expiredAt = Math.floor(Date.now() / 1000) - 60;
		server.use(
			http.get("/api/v1/clients/c1/tokens", () =>
				HttpResponse.json({
					items: [
						{
							id: "tok-exp",
							prefix: "veil_ex",
							label: "old phone",
							enabled: true,
							createdAt: 1700000000,
							expiresAt: expiredAt,
						},
					],
				}),
			),
			http.post(
				"/api/v1/clients/c1/tokens/tok-exp/rotate",
				async ({ request }) => {
					const text = await request.text();
					rotateBodies.push(text ? JSON.parse(text) : undefined);
					return HttpResponse.json({
						token: {
							id: "tok-exp",
							prefix: "veil_nw",
							label: "old phone",
							enabled: true,
							createdAt: 1700000000,
						},
						plaintext: "veil_nw_secret",
						url: "/s/veil_nw_secret",
					});
				},
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<SubscriptionTokensPanel clientId="c1" />
				</I18nProvider>
			</QueryClientProvider>,
		);
		expect(await screen.findByText(/^expired$/i)).toBeInTheDocument();
		expect(screen.queryByText(/^active$/i)).not.toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /^rotate$/i }));
		expect(rotateBodies).toEqual([]);
		fireEvent.change(await screen.findByLabelText(/new expiry/i), {
			target: { value: "2099-01-01" },
		});
		await user.click(
			screen.getByRole("button", { name: /rotate with new expiry/i }),
		);
		await waitFor(() => expect(rotateBodies).toHaveLength(1));
		const body = rotateBodies[0] as { expiresAt?: number };
		expect(body.expiresAt).toBeGreaterThan(Date.now() / 1000);
	});
});
