import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider, useAuth } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { UsersPage } from "../pages/UsersPage";
import { HttpResponse, http, server } from "./server";

function SessionProbe() {
	const { session } = useAuth();
	return <output data-testid="session-probe">{JSON.stringify(session)}</output>;
}

function renderUsers() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<UsersPage />
					<SessionProbe />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

describe("UsersPage errors", () => {
	it("does not treat a failed user list as empty", async () => {
		server.use(
			http.get("/api/users", () =>
				HttpResponse.json(
					{ error: { message: "users down" } },
					{ status: 500 },
				),
			),
			http.get("/api/auth/sessions", () =>
				HttpResponse.json(
					{ error: { message: "sessions down" } },
					{ status: 500 },
				),
			),
		);
		renderUsers();
		expect(await screen.findByText(/users down/i)).toBeInTheDocument();
		expect(await screen.findByText(/sessions down/i)).toBeInTheDocument();
		expect(screen.queryByText(/no active sessions/i)).not.toBeInTheDocument();
	});

	it("does not PUT when Save echoes the current role with no password", async () => {
		const puts: string[] = [];
		server.use(
			http.get("/api/users", () =>
				HttpResponse.json([
					{ username: "alice", role: "viewer", locale: "en" },
				]),
			),
			http.get("/api/auth/sessions", () => HttpResponse.json([])),
			http.put("/api/users/:name", async ({ request }) => {
				puts.push(await request.text());
				return HttpResponse.json({ success: true });
			}),
		);
		renderUsers();
		fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));
		fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
		await waitFor(() =>
			expect(
				screen.queryByRole("button", { name: /^save$/i }),
			).not.toBeInTheDocument(),
		);
		expect(puts).toEqual([]);
	});

	// #727: PUT /api/users/{name} revokes every session of that user — the
	// "updated" notice must say so, like the delete dialog already does.
	it("says the user's sessions were revoked after a successful update", async () => {
		server.use(
			http.get("/api/users", () =>
				HttpResponse.json([
					{ username: "admin", role: "admin", locale: "en" },
					{ username: "bob", role: "viewer", locale: "en" },
				]),
			),
			http.get("/api/auth/sessions", () => HttpResponse.json([])),
			http.put("/api/users/bob", () => HttpResponse.json({ success: true })),
		);
		renderUsers();
		const edits = await screen.findAllByRole("button", { name: /^edit$/i });
		// Row order follows the user list: admin first, bob second.
		const bobEdit = edits[1];
		expect(bobEdit).toBeDefined();
		if (!bobEdit) return;
		fireEvent.click(bobEdit);
		fireEvent.change(screen.getByLabelText(/new password/i), {
			target: { value: "new-secret-pass-1" },
		});
		fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
		expect(
			await screen.findByText(/user bob updated\. .*sessions were revoked/i),
		).toBeInTheDocument();
	});

	// #693: editing YOURSELF revokes your own session — the form warns before
	// save and the success path signs out instead of painting "updated".
	it("warns about self-edit and signs out instead of painting updated", async () => {
		let logoutCalls = 0;
		server.use(
			http.get("/api/users", () =>
				HttpResponse.json([
					{ username: "admin", role: "admin", locale: "en" },
					{ username: "bob", role: "viewer", locale: "en" },
				]),
			),
			http.get("/api/auth/sessions", () => HttpResponse.json([])),
			http.put("/api/users/admin", () => HttpResponse.json({ success: true })),
			http.post("/api/auth/logout", () => {
				logoutCalls += 1;
				return HttpResponse.json({ ok: true });
			}),
		);
		renderUsers();
		await waitFor(() =>
			expect(screen.getByTestId("session-probe")).toHaveTextContent(
				'"authenticated":true',
			),
		);
		const adminEdit = (
			await screen.findAllByRole("button", { name: /^edit$/i })
		)[0];
		expect(adminEdit).toBeDefined();
		if (!adminEdit) return;
		fireEvent.click(adminEdit);
		// Pre-save warning is visible while editing your own account.
		expect(
			await screen.findByText(/revokes your sessions.*signed out/i),
		).toBeInTheDocument();
		fireEvent.change(screen.getByLabelText(/new password/i), {
			target: { value: "new-secret-pass-1" },
		});
		fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
		await waitFor(() => expect(logoutCalls).toBe(1));
		await waitFor(() =>
			expect(screen.getByTestId("session-probe")).toHaveTextContent(
				'"authenticated":false',
			),
		);
		expect(screen.queryByText(/user admin updated/i)).not.toBeInTheDocument();
	});

	// #730: sessions die at min(idleExpiresAt, expiresAt) — the idle deadline
	// (~30m) is the binding constraint and must be painted, not dropped.
	it("shows the idle expiry deadline for active sessions", async () => {
		const idle = "2030-06-01T00:30:00Z";
		const absolute = "2030-06-02T00:00:00Z";
		server.use(
			http.get("/api/users", () => HttpResponse.json([])),
			http.get("/api/auth/sessions", () =>
				HttpResponse.json([
					{
						id: "0123456789abcdef",
						username: "admin",
						role: "admin",
						createdAt: "2030-06-01T00:00:00Z",
						lastSeenAt: "2030-06-01T00:00:00Z",
						idleExpiresAt: idle,
						expiresAt: absolute,
						userAgent: "agent",
						current: true,
					},
				]),
			),
		);
		renderUsers();
		expect(await screen.findByText(/idle expires/i)).toBeInTheDocument();
		expect(
			await screen.findByText(new Date(idle).toLocaleString()),
		).toBeInTheDocument();
		expect(
			screen.getByText(new Date(absolute).toLocaleString()),
		).toBeInTheDocument();
	});

	// #702: session revoke is a remote sign-out — the DELETE must not fire
	// until the confirm dialog action, like the user Delete next to it.
	it("does not DELETE a session until revoke is confirmed", async () => {
		const user = userEvent.setup();
		const deletes: string[] = [];
		server.use(
			http.get("/api/users", () =>
				HttpResponse.json([
					{ username: "alice", role: "viewer", locale: "en" },
				]),
			),
			http.get("/api/auth/sessions", () =>
				HttpResponse.json([
					{
						id: "s-current",
						username: "admin",
						role: "admin",
						createdAt: "2024-01-01T00:00:00Z",
						lastSeenAt: "2024-01-01T00:00:00Z",
						expiresAt: "2024-01-02T00:00:00Z",
						current: true,
					},
					{
						id: "s-other",
						username: "alice",
						role: "viewer",
						createdAt: "2024-01-01T00:00:00Z",
						lastSeenAt: "2024-01-01T00:00:00Z",
						expiresAt: "2024-01-02T00:00:00Z",
						userAgent: "curl/8",
						current: false,
					},
				]),
			),
			http.delete("/api/auth/sessions", async ({ request }) => {
				deletes.push(await request.text());
				return HttpResponse.json({ success: true });
			}),
		);
		renderUsers();
		await user.click(await screen.findByRole("button", { name: /^revoke$/i }));
		expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
		expect(deletes).toEqual([]);
		await user.click(screen.getByRole("button", { name: /confirm revoke/i }));
		await waitFor(() => expect(deletes).toHaveLength(1));
		expect(deletes[0]).toContain("s-other");
	});
});
