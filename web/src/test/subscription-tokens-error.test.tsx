import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
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
});
