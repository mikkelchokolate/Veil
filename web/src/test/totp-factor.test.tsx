import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider, useAuth } from "../auth/AuthContext";
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

// Surfaces the loaded session's second-factor mark so tests can wait for the
// auth status before acting on the card's credential gate (#1250).
function SessionProbe() {
	const { session } = useAuth();
	return session?.authenticated ? (
		<span data-testid="session-probe">
			{session.secondFactor ? "marked" : "unmarked"}
		</span>
	) : null;
}

function renderTotpCard() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<SessionProbe />
					<TotpCard />
				</I18nProvider>
			</AuthProvider>
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

describe("TOTP enroll credential gate (#1250)", () => {
	it("asks an unmarked session for the account password before enrolling", async () => {
		const bodies: string[] = [];
		server.use(
			http.post("/api/v1/users/me/totp/enroll", async ({ request }) => {
				bodies.push(await request.text());
				return HttpResponse.json({
					secret: "JBSWY3DPEHPK3PXP",
					otpauthUri: "otpauth://totp/Veil:admin?secret=JBSWY3DPEHPK3PXP",
					issuer: "Veil",
				});
			}),
		);
		renderTotpCard();
		const user = userEvent.setup();
		expect(await screen.findByTestId("session-probe")).toHaveTextContent(
			"unmarked",
		);
		await user.click(
			await screen.findByRole("button", { name: /enable authenticator/i }),
		);
		// No seed is requested until the password is supplied.
		expect(bodies).toHaveLength(0);
		const submit = screen.getByRole("button", { name: /^confirm$/i });
		expect(submit).toBeDisabled();
		await user.type(screen.getByLabelText(/password/i), "hunter2-password");
		await user.click(submit);
		await waitFor(() => expect(bodies).toHaveLength(1));
		expect(JSON.parse(bodies[0])).toEqual({ password: "hunter2-password" });
		expect(
			await screen.findByDisplayValue("JBSWY3DPEHPK3PXP"),
		).toBeInTheDocument();
	});

	it("enrolls a second-factor-marked session without a password prompt", async () => {
		const bodies: string[] = [];
		server.use(
			http.get("/api/auth/status", () =>
				HttpResponse.json({
					authenticated: true,
					username: "admin",
					role: "admin",
					csrfToken: "test-csrf",
					secondFactor: true,
				}),
			),
			http.post("/api/v1/users/me/totp/enroll", async ({ request }) => {
				bodies.push(await request.text());
				return HttpResponse.json({
					secret: "JBSWY3DPEHPK3PXP",
					otpauthUri: "otpauth://totp/Veil:admin?secret=JBSWY3DPEHPK3PXP",
					issuer: "Veil",
				});
			}),
		);
		renderTotpCard();
		const user = userEvent.setup();
		expect(await screen.findByTestId("session-probe")).toHaveTextContent(
			"marked",
		);
		await user.click(
			await screen.findByRole("button", { name: /enable authenticator/i }),
		);
		await waitFor(() => expect(bodies).toHaveLength(1));
		expect(bodies[0]).toBe("");
		expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
	});
});
