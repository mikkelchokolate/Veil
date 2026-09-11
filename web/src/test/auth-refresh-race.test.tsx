import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { apiFetch } from "../api/fetcher";
import { AuthProvider, useAuth } from "../auth/AuthContext";
import { HttpResponse, http, server } from "./server";

type SessionBody = {
	authenticated: boolean;
	username?: string;
	role?: string;
	locale?: string;
	csrfToken?: string;
};

function createGate() {
	let release!: () => void;
	const promise = new Promise<void>((resolve) => {
		release = resolve;
	});
	return { promise, release };
}

function Probe() {
	const auth = useAuth();
	const session = auth.session;
	return (
		<div>
			<output data-testid="session">
				{JSON.stringify({
					authenticated: session?.authenticated,
					role: session?.role,
					csrfToken: session?.csrfToken,
				})}
			</output>
			<output data-testid="loading">{String(auth.loading)}</output>
			<button type="button" onClick={() => void auth.refresh()}>
				refresh
			</button>
			<button type="button" onClick={() => void auth.logout()}>
				logout
			</button>
			<button type="button" onClick={() => void auth.login("admin", "secret")}>
				login
			</button>
			<button
				type="button"
				onClick={() => {
					void apiFetch("/api/v1/probe", { method: "POST", body: "{}" }).catch(
						() => undefined,
					);
				}}
			>
				probe-csrf
			</button>
		</div>
	);
}

function renderAuth() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<Probe />
			</AuthProvider>
		</QueryClientProvider>,
	);
}

describe("auth refresh races", () => {
	it("ignores a pre-logout status response that arrives after logout", async () => {
		const user = userEvent.setup();
		let session: SessionBody = {
			authenticated: true,
			username: "admin",
			role: "admin",
			csrfToken: "csrf-stale",
		};
		const holds: Promise<void>[] = [];
		let inFlight = 0;
		let seenCsrf: string | null | undefined;
		server.use(
			http.get("*/api/auth/status", async () => {
				const snapshot = { ...session };
				const gate = holds.shift();
				if (gate) {
					inFlight += 1;
					await gate;
					inFlight -= 1;
				}
				return HttpResponse.json(snapshot);
			}),
			http.post("*/api/auth/logout", () => {
				session = { authenticated: false };
				return new HttpResponse(null, { status: 204 });
			}),
			http.post("*/api/v1/probe", ({ request }) => {
				seenCsrf = request.headers.get("X-CSRF-Token");
				return HttpResponse.json({ ok: true });
			}),
		);
		renderAuth();
		await waitFor(() =>
			expect(screen.getByTestId("session")).toHaveTextContent(
				'"authenticated":true',
			),
		);
		expect(screen.getByTestId("loading")).toHaveTextContent("false");

		const held = createGate();
		holds.push(held.promise);
		await user.click(screen.getByRole("button", { name: "refresh" }));
		await waitFor(() => expect(inFlight).toBe(1));
		await user.click(screen.getByRole("button", { name: "logout" }));
		await waitFor(() =>
			expect(screen.getByTestId("session")).toHaveTextContent(
				'"authenticated":false',
			),
		);

		await act(async () => {
			held.release();
			await new Promise((resolve) => setTimeout(resolve, 50));
		});
		expect(screen.getByTestId("session")).toHaveTextContent(
			'"authenticated":false',
		);
		expect(screen.getByTestId("session")).not.toHaveTextContent("csrf-stale");
		expect(screen.getByTestId("loading")).toHaveTextContent("false");

		await user.click(screen.getByRole("button", { name: "probe-csrf" }));
		await waitFor(() =>
			expect(seenCsrf === null || seenCsrf === undefined).toBe(true),
		);
	});

	it("ignores a pre-login unauthenticated status that arrives after login", async () => {
		const user = userEvent.setup();
		let session: SessionBody = { authenticated: false };
		const holds: Promise<void>[] = [];
		let inFlight = 0;
		server.use(
			http.get("*/api/auth/status", async () => {
				const snapshot = { ...session };
				const gate = holds.shift();
				if (gate) {
					inFlight += 1;
					await gate;
					inFlight -= 1;
				}
				return HttpResponse.json(snapshot);
			}),
			http.post("*/api/auth/login", () => {
				session = {
					authenticated: true,
					username: "admin",
					role: "admin",
					csrfToken: "csrf-login",
				};
				return HttpResponse.json({
					csrfToken: "csrf-login",
					username: "admin",
					role: "admin",
				});
			}),
		);
		renderAuth();
		await waitFor(() =>
			expect(screen.getByTestId("session")).toHaveTextContent(
				'"authenticated":false',
			),
		);

		const held = createGate();
		holds.push(held.promise);
		await user.click(screen.getByRole("button", { name: "refresh" }));
		await waitFor(() => expect(inFlight).toBe(1));
		await user.click(screen.getByRole("button", { name: "login" }));
		await waitFor(() =>
			expect(screen.getByTestId("session")).toHaveTextContent(
				'"authenticated":true',
			),
		);
		expect(screen.getByTestId("session")).toHaveTextContent("csrf-login");

		await act(async () => {
			held.release();
			await new Promise((resolve) => setTimeout(resolve, 50));
		});
		expect(screen.getByTestId("session")).toHaveTextContent(
			'"authenticated":true',
		);
		expect(screen.getByTestId("session")).toHaveTextContent("csrf-login");
		expect(screen.getByTestId("loading")).toHaveTextContent("false");
	});

	it("applies overlapping refreshes in start order, not completion order", async () => {
		const user = userEvent.setup();
		let session: SessionBody = {
			authenticated: true,
			username: "admin",
			role: "admin",
			csrfToken: "csrf-one",
		};
		const holds: Promise<void>[] = [];
		let inFlight = 0;
		server.use(
			http.get("*/api/auth/status", async () => {
				const snapshot = { ...session };
				const gate = holds.shift();
				if (gate) {
					inFlight += 1;
					await gate;
					inFlight -= 1;
				}
				return HttpResponse.json(snapshot);
			}),
		);
		renderAuth();
		await waitFor(() =>
			expect(screen.getByTestId("session")).toHaveTextContent("csrf-one"),
		);

		const first = createGate();
		holds.push(first.promise);
		await user.click(screen.getByRole("button", { name: "refresh" }));
		await waitFor(() => expect(inFlight).toBe(1));
		session = {
			authenticated: true,
			username: "viewer",
			role: "viewer",
			csrfToken: "csrf-two",
		};
		const second = createGate();
		holds.push(second.promise);
		await user.click(screen.getByRole("button", { name: "refresh" }));
		await waitFor(() => expect(inFlight).toBeGreaterThanOrEqual(1));

		await act(async () => {
			second.release();
		});
		await waitFor(() =>
			expect(screen.getByTestId("session")).toHaveTextContent("csrf-two"),
		);
		await act(async () => {
			first.release();
			await new Promise((resolve) => setTimeout(resolve, 50));
		});
		expect(screen.getByTestId("session")).toHaveTextContent("csrf-two");
		expect(screen.getByTestId("session")).toHaveTextContent('"role":"viewer"');
		expect(screen.getByTestId("loading")).toHaveTextContent("false");
	});

	it("still clears the session when refresh completes before logout", async () => {
		const user = userEvent.setup();
		let session: SessionBody = {
			authenticated: true,
			username: "admin",
			role: "admin",
			csrfToken: "csrf-one",
		};
		server.use(
			http.get("*/api/auth/status", () => HttpResponse.json(session)),
			http.post("*/api/auth/logout", () => {
				session = { authenticated: false };
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderAuth();
		await waitFor(() =>
			expect(screen.getByTestId("session")).toHaveTextContent(
				'"authenticated":true',
			),
		);
		await user.click(screen.getByRole("button", { name: "refresh" }));
		await waitFor(() =>
			expect(screen.getByTestId("session")).toHaveTextContent("csrf-one"),
		);
		await user.click(screen.getByRole("button", { name: "logout" }));
		await waitFor(() =>
			expect(screen.getByTestId("session")).toHaveTextContent(
				'"authenticated":false',
			),
		);
	});
});
