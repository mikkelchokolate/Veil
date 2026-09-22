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

async function confirmRotate() {
	fireEvent.click(
		await screen.findByRole("button", { name: /rotate state key/i }),
	);
	fireEvent.click(
		await screen.findByRole("button", { name: /confirm rotation/i }),
	);
}

describe("SettingsPage key rotation", () => {
	// #731: the API reports revokedSessions — the notice must report the real
	// count, not blanket-claim "other sessions were revoked".
	it("does not claim revocations when no other session was active", async () => {
		server.use(
			http.post("/api/admin/rotate-key", () =>
				HttpResponse.json({ success: true, revokedSessions: 0 }),
			),
		);
		renderSettings();
		await confirmRotate();
		expect(
			await screen.findByText(/no other sessions were active/i),
		).toBeInTheDocument();
		expect(
			screen.queryByText(/other sessions were revoked/i),
		).not.toBeInTheDocument();
	});

	it("reports the real revoked session count", async () => {
		server.use(
			http.post("/api/admin/rotate-key", () =>
				HttpResponse.json({ success: true, revokedSessions: 2 }),
			),
		);
		renderSettings();
		await confirmRotate();
		expect(
			await screen.findByText(/revoked 2 other session/i),
		).toBeInTheDocument();
	});

	// #726: on success=false the revocation already happened — the notice
	// must not talk only about the failed apply.
	it("keeps session revocation visible when the post-rotate apply fails", async () => {
		server.use(
			http.post("/api/admin/rotate-key", () =>
				HttpResponse.json({
					success: false,
					revokedSessions: 2,
					revision: { desired: 2, applied: 1, state: "drift" },
				}),
			),
		);
		renderSettings();
		await confirmRotate();
		expect(
			await screen.findByText(
				/2 other session\(s\) were revoked.*applying the new revision failed/i,
			),
		).toBeInTheDocument();
	});
});
