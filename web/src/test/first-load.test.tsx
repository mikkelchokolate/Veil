import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { App } from "../App";
import { HttpResponse, http, server } from "./server";

describe("unauthenticated first-load gate", () => {
	it("shows the login form immediately instead of a loading spinner", () => {
		let release!: () => void;
		const blocked = new Promise<void>((resolve) => {
			release = resolve;
		});
		server.use(
			http.get("/api/setup/status", async () => {
				await blocked;
				return HttpResponse.json({ required: false, allowed: false });
			}),
			http.get("/api/auth/status", async () => {
				await blocked;
				return HttpResponse.json({ authenticated: false });
			}),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<App />
			</QueryClientProvider>,
		);
		expect(screen.getByRole("heading", { name: "Veil" })).toBeInTheDocument();
		expect(screen.getByLabelText("Username")).toBeInTheDocument();
		expect(screen.getByLabelText("Password")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
		expect(screen.queryByLabelText("Loading…")).not.toBeInTheDocument();
		release();
	});
});
