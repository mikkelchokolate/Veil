import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { UsersPage } from "../pages/UsersPage";
import { HttpResponse, http, server } from "./server";

function renderUsers() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<UsersPage />
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
});
