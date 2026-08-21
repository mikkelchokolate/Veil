import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "../auth/AuthContext";
import { LoginView } from "../auth/LoginView";
import { I18nProvider } from "../i18n/I18nContext";
import { captureLoginFields } from "../pendingLogin";
import { HttpResponse, http, server } from "./server";

function renderLogin() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<LoginView />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

describe("static login handoff", () => {
	it("captures typed credentials from the first-load form", () => {
		document.body.innerHTML = `
			<form id="f">
				<input id="login-username" value="admin" />
				<input id="login-password" type="password" value="s3cret" />
			</form>
		`;
		captureLoginFields(true);
		expect(window.__VEIL_PENDING_LOGIN).toEqual({
			username: "admin",
			password: "s3cret",
			submit: true,
		});
		captureLoginFields(false);
		expect(window.__VEIL_PENDING_LOGIN).toEqual({
			username: "admin",
			password: "s3cret",
			submit: true,
		});
		delete window.__VEIL_PENDING_LOGIN;
		document.body.innerHTML = "";
	});

	it("signs in with captured credentials when the SPA mounts", async () => {
		window.__VEIL_PENDING_LOGIN = {
			username: "admin",
			password: "s3cret-pass",
			submit: true,
		};
		let body: Record<string, unknown> | null = null;
		server.use(
			http.get("/api/auth/status", () =>
				HttpResponse.json({ authenticated: false }),
			),
			http.post("/api/auth/login", async ({ request }) => {
				body = (await request.json()) as Record<string, unknown>;
				return HttpResponse.json({ csrfToken: "csrf" });
			}),
		);
		renderLogin();
		await waitFor(() => expect(body).not.toBeNull());
		expect(body).toEqual({ username: "admin", password: "s3cret-pass" });
		expect(
			screen.getByRole("button", { name: /signing in|sign in/i }),
		).toBeInTheDocument();
	});

	it("does not call a Panel outage invalid credentials", async () => {
		const user = userEvent.setup();
		server.use(
			http.get("/api/auth/status", () =>
				HttpResponse.json({ authenticated: false }),
			),
			http.post("/api/auth/login", () =>
				HttpResponse.json(
					{ error: { message: "unavailable" } },
					{ status: 503 },
				),
			),
		);
		renderLogin();
		await user.type(screen.getByLabelText("Username"), "admin");
		await user.type(screen.getByLabelText("Password"), "s3cret-pass");
		await user.click(screen.getByRole("button", { name: /^sign in$/i }));
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent(/could not sign in|не удалось войти/i);
		expect(alert).not.toHaveTextContent(/invalid username or password/i);
	});
});
