import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { App } from "../App";
import { HttpResponse, http, server } from "./server";

describe("setup status errors", () => {
	it("does not send the operator to Sign in when setup status fails", async () => {
		server.use(
			http.get("/api/setup/status", () =>
				HttpResponse.json(
					{ error: { message: "setup down" } },
					{ status: 500 },
				),
			),
			http.get("/api/auth/status", () =>
				HttpResponse.json({ authenticated: false }),
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<App />
			</QueryClientProvider>,
		);
		expect(await screen.findByRole("alert")).toHaveTextContent(/setup down/i);
		expect(
			screen.queryByRole("button", { name: /^sign in$/i }),
		).not.toBeInTheDocument();
	});
});
