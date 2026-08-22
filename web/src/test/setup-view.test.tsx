import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "../App";
import { AuthProvider } from "../auth/AuthContext";
import { SetupView } from "../auth/SetupView";
import { I18nProvider } from "../i18n/I18nContext";
import { HttpResponse, http, server } from "./server";

function renderSetup() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<SetupView />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

describe("SetupView", () => {
	it("requires backup acknowledgement and posts backupAcknowledged", async () => {
		const user = userEvent.setup();
		let body: Record<string, unknown> | null = null;
		server.use(
			http.post("/api/setup/complete", async ({ request }) => {
				body = (await request.json()) as Record<string, unknown>;
				return HttpResponse.json({ completed: true }, { status: 201 });
			}),
		);
		renderSetup();
		await user.clear(screen.getByLabelText("Username"));
		await user.type(screen.getByLabelText("Username"), "admin");
		await user.type(
			screen.getByLabelText("Password"),
			"a-long-secure-password",
		);
		await user.type(
			screen.getByLabelText("Confirm password"),
			"a-long-secure-password",
		);
		await user.click(
			screen.getByRole("checkbox", {
				name: /preserve both the encrypted state/i,
			}),
		);
		await user.click(
			screen.getByRole("button", { name: "Create administrator" }),
		);
		await waitFor(() => expect(body).not.toBeNull());
		expect(body).toMatchObject({
			username: "admin",
			password: "a-long-secure-password",
			backupAcknowledged: true,
		});
	});

	it("leaves setup when complete returns already-finished 409", async () => {
		const user = userEvent.setup();
		let setupRequired = true;
		server.use(
			http.get("/api/setup/status", () =>
				HttpResponse.json({
					required: setupRequired,
					allowed: true,
					completed: !setupRequired,
				}),
			),
			http.get("/api/auth/status", () =>
				HttpResponse.json({ authenticated: false }),
			),
			http.post("/api/setup/complete", () => {
				setupRequired = false;
				return HttpResponse.json(
					{ error: { message: "setup already complete" } },
					{ status: 409 },
				);
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
		await screen.findByRole("button", { name: "Create administrator" });
		await user.type(
			screen.getByLabelText(/^password$/i),
			"a-long-secure-password",
		);
		await user.type(
			screen.getByLabelText(/^confirm password$/i),
			"a-long-secure-password",
		);
		await user.click(
			screen.getByRole("checkbox", {
				name: /preserve both the encrypted state/i,
			}),
		);
		await user.click(
			screen.getByRole("button", { name: "Create administrator" }),
		);
		expect(
			await screen.findByRole("button", { name: /^sign in$/i }),
		).toBeInTheDocument();
	});
});
