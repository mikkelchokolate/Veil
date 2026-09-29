import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "../auth/AuthContext";
import { LoginView } from "../auth/LoginView";
import { I18nProvider } from "../i18n/I18nContext";
import { TotpCard } from "../pages/SettingsTotp";
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

function renderTotpCard() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<I18nProvider>
				<TotpCard />
			</I18nProvider>
		</QueryClientProvider>,
	);
}

const enabledStatus = {
	enabled: true,
	pendingEnrollment: false,
	recoveryCodesRemaining: 10,
};

describe("TOTP second-factor login step (#1172)", () => {
	it("shows the pending challenge expiry and offers back-to-password", async () => {
		const expiresAt = new Date(Date.now() + 5 * 60_000).toISOString();
		server.use(
			http.post("/api/auth/login", () =>
				HttpResponse.json({
					success: true,
					secondFactorRequired: true,
					secondFactorMethods: ["totp"],
					pendingExpiresAt: expiresAt,
				}),
			),
		);
		const user = userEvent.setup();
		renderLogin();
		await user.type(screen.getByLabelText(/username/i), "alice");
		await user.type(screen.getByLabelText(/password/i), "pw");
		await user.click(screen.getByRole("button", { name: /^sign in$/i }));

		// Factor step: expiry hint rendered from pendingExpiresAt, and a way
		// back exists for when the 5-minute challenge lapses.
		expect(
			await screen.findByText(/this challenge expires at/i),
		).toBeInTheDocument();
		const back = screen.getByRole("button", { name: /back to sign in/i });
		await user.click(back);
		expect(await screen.findByLabelText(/password/i)).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /back to sign in/i }),
		).not.toBeInTheDocument();
	});

	it("posts the factor code to the verify endpoint", async () => {
		const bodies: string[] = [];
		server.use(
			http.post("/api/auth/login", () =>
				HttpResponse.json({
					success: true,
					secondFactorRequired: true,
					secondFactorMethods: ["totp"],
				}),
			),
			http.post("/api/v1/auth/totp/verify", async ({ request }) => {
				bodies.push(await request.text());
				return HttpResponse.json(
					{ error: { message: "invalid verification code" } },
					{ status: 401 },
				);
			}),
		);
		const user = userEvent.setup();
		renderLogin();
		await user.type(screen.getByLabelText(/username/i), "alice");
		await user.type(screen.getByLabelText(/password/i), "pw");
		await user.click(screen.getByRole("button", { name: /^sign in$/i }));
		await user.type(
			await screen.findByLabelText(/authenticator code/i),
			"123456",
		);
		await user.click(screen.getByRole("button", { name: /verify/i }));
		await waitFor(() => expect(bodies).toHaveLength(1));
		expect(JSON.parse(bodies[0])).toEqual({ code: "123456" });
	});
});

describe("TOTP self-disable dialog (#1172)", () => {
	it("requires a live authenticator code — no password field", async () => {
		server.use(
			http.get("/api/v1/users/me/totp", () => HttpResponse.json(enabledStatus)),
		);
		renderTotpCard();
		const user = userEvent.setup();
		fireEvent.click(await screen.findByRole("button", { name: /disable/i }));
		// Factor-grade disable: the dialog must not offer the password path
		// at all, and submits only a live code.
		expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
		const submit = screen.getByRole("button", { name: /^disable$/i });
		expect(submit).toBeDisabled();

		const bodies: string[] = [];
		server.use(
			http.delete("/api/v1/users/me/totp", async ({ request }) => {
				bodies.push(await request.text());
				return HttpResponse.json({ success: true });
			}),
		);
		await user.type(screen.getByLabelText(/authenticator code/i), "654321");
		await user.click(screen.getByRole("button", { name: /^disable$/i }));
		await waitFor(() => expect(bodies).toHaveLength(1));
		expect(JSON.parse(bodies[0])).toEqual({ code: "654321" });
	});

	it("labels the enroll button as a restart while enrollment is pending", async () => {
		server.use(
			http.get("/api/v1/users/me/totp", () =>
				HttpResponse.json({
					enabled: false,
					pendingEnrollment: true,
					recoveryCodesRemaining: 0,
				}),
			),
		);
		renderTotpCard();
		// A mid-enroll refresh loses the QR/secret local state; the pending
		// flag must turn the action into an explicit restart.
		expect(
			await screen.findByRole("button", { name: /restart enrollment/i }),
		).toBeInTheDocument();
	});
});
