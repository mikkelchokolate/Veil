import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { SettingsPage } from "../pages/SettingsPage";
import { HttpResponse, http, server } from "./server";

function renderSettings() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<SettingsPage />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

// #676: POST /api/admin/rotate-key is a MutationOutcome-bearing mutation —
// success=false on a 200 means the key rotation committed but the auto-apply
// did not converge. The notice must say that, never the clean "rotated" copy.
describe("SettingsPage rotate key apply outcome", () => {
	it("surfaces a committed-but-unapplied rotation instead of claiming a clean rotate", async () => {
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({
					mode: "prod",
					panelListen: "127.0.0.1:2096",
				}),
			),
			http.post("/api/admin/rotate-key", () =>
				HttpResponse.json({
					success: false,
					revokedSessions: 2,
					revision: { desired: 2, applied: 1, state: "failed" },
					applyJob: {
						id: "job-rot",
						desiredRevision: 2,
						baseRevision: 1,
						status: "failed",
						trigger: "mutation",
						createdAt: 1700000000,
					},
				}),
			),
		);
		renderSettings();
		fireEvent.click(
			await screen.findByRole("button", { name: /rotate state key/i }),
		);
		fireEvent.click(
			await screen.findByRole("button", { name: /confirm rotation/i }),
		);
		expect(
			await screen.findByText(
				/state key rotated, but applying the new revision failed/i,
			),
		).toBeInTheDocument();
		expect(
			screen.queryByText(/other sessions were revoked/i),
		).not.toBeInTheDocument();
	});

	it("keeps the clean rotated notice when the apply converges", async () => {
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({
					mode: "prod",
					panelListen: "127.0.0.1:2096",
				}),
			),
			http.post("/api/admin/rotate-key", () =>
				HttpResponse.json({
					success: true,
					revokedSessions: 2,
					revision: { desired: 2, applied: 2, state: "synced" },
				}),
			),
		);
		renderSettings();
		fireEvent.click(
			await screen.findByRole("button", { name: /rotate state key/i }),
		);
		fireEvent.click(
			await screen.findByRole("button", { name: /confirm rotation/i }),
		);
		expect(
			await screen.findByText(
				/state key rotated\. other sessions were revoked/i,
			),
		).toBeInTheDocument();
	});
});
