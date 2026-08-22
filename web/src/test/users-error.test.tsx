import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
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
});
