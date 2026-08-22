import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
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
});
