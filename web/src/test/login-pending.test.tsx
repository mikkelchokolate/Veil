import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { apiFetch } from "../api/fetcher";
import { AuthProvider, useAuth } from "../auth/AuthContext";
import { LoginView } from "../auth/LoginView";
import { I18nProvider } from "../i18n/I18nContext";
import { captureLoginFields } from "../pendingLogin";
import { HttpResponse, http, server } from "./server";

function SessionProbe() {
	const { session } = useAuth();
	return <output data-testid="session-probe">{JSON.stringify(session)}</output>;
}

function FetchClientsButton() {
	return (
		<button
			type="button"
			onClick={() => {
				void apiFetch("/api/v1/clients").catch(() => undefined);
			}}
		>
			fetch-clients
		</button>
	);
}

function renderLogin() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<LoginView />
					<SessionProbe />
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

	it("keeps the login CSRF token when status is briefly unavailable", async () => {
		const user = userEvent.setup();
		server.use(
			http.get("/api/auth/status", () =>
				HttpResponse.json({ error: { message: "internal" } }, { status: 500 }),
			),
			http.post("/api/auth/login", () =>
				HttpResponse.json({
					csrfToken: "csrf-after-login",
					username: "admin",
					role: "admin",
					locale: "en",
					success: true,
				}),
			),
		);
		renderLogin();
		await user.type(screen.getByLabelText("Username"), "admin");
		await user.type(screen.getByLabelText("Password"), "s3cret-pass");
		await user.click(screen.getByRole("button", { name: /^sign in$/i }));
		await waitFor(() =>
			expect(screen.getByTestId("session-probe")).toHaveTextContent(
				'"authenticated":true',
			),
		);
		expect(screen.getByTestId("session-probe")).toHaveTextContent(
			'"csrfToken":"csrf-after-login"',
		);
	});

	it("returns to Sign in when a later API call is 401", async () => {
		const user = userEvent.setup();
		server.use(
			http.get("/api/auth/status", () =>
				HttpResponse.json({
					authenticated: true,
					username: "admin",
					role: "admin",
					csrfToken: "csrf",
				}),
			),
			http.get("/api/v1/clients", () =>
				HttpResponse.json(
					{ error: { message: "unauthorized" } },
					{ status: 401 },
				),
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<AuthProvider>
					<I18nProvider>
						<SessionProbe />
						<FetchClientsButton />
					</I18nProvider>
				</AuthProvider>
			</QueryClientProvider>,
		);
		await waitFor(() =>
			expect(screen.getByTestId("session-probe")).toHaveTextContent(
				'"authenticated":true',
			),
		);
		await user.click(screen.getByRole("button", { name: "fetch-clients" }));
		await waitFor(() =>
			expect(screen.getByTestId("session-probe")).toHaveTextContent(
				'"authenticated":false',
			),
		);
	});

	it("keeps the Sign in error when I18nProvider recreates t() during a failing pending login", async () => {
		const user = userEvent.setup();
		window.__VEIL_PENDING_LOGIN = {
			username: "admin",
			password: "s3cret-pass",
			submit: true,
		};
		let posts = 0;
		let release!: () => void;
		const held = new Promise<void>((resolve) => {
			release = resolve;
		});
		server.use(
			http.get("/api/auth/status", () =>
				HttpResponse.json({ authenticated: false }),
			),
			http.post("/api/auth/login", async () => {
				posts += 1;
				await held;
				return HttpResponse.json(
					{ error: { message: "unauthorized" } },
					{ status: 401 },
				);
			}),
		);
		function Harness() {
			const [, setN] = useState(0);
			return (
				<>
					<button type="button" onClick={() => setN((n) => n + 1)}>
						recreate-i18n
					</button>
					<I18nProvider>
						<LoginView />
					</I18nProvider>
				</>
			);
		}
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<AuthProvider>
					<Harness />
				</AuthProvider>
			</QueryClientProvider>,
		);
		expect(
			await screen.findByRole("button", { name: /signing in/i }),
		).toBeDisabled();
		await user.click(screen.getByRole("button", { name: "recreate-i18n" }));
		release();
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent(/invalid username or password/i);
		const signIn = await screen.findByRole("button", { name: /^sign in$/i });
		expect(signIn).toBeEnabled();
		expect(posts).toBe(1);
		delete window.__VEIL_PENDING_LOGIN;
	});
});
