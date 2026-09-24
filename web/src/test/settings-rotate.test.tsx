import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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
	// #766: rotation is already gated behind the AlertDialog — the primary
	// button only opens it, so no POST may fire until Confirm is clicked
	// (same no-request-until-confirm lock as #733 / #757).
	it("does not POST rotate-key until the dialog is confirmed", async () => {
		const posts: string[] = [];
		server.use(
			http.post("/api/admin/rotate-key", () => {
				posts.push("rotate-key");
				return HttpResponse.json({ success: true, revokedSessions: 0 });
			}),
		);
		renderSettings();
		fireEvent.click(
			await screen.findByRole("button", { name: /rotate state key/i }),
		);
		expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
		expect(posts).toEqual([]);
		fireEvent.click(screen.getByRole("button", { name: /confirm rotation/i }));
		await waitFor(() => expect(posts).toEqual(["rotate-key"]));
	});

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
					revision: { desired: 2, applied: 1, state: "failed" },
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
		await confirmRotate();
		expect(
			await screen.findByText(
				/2 other session\(s\) were revoked.*applying the new revision failed/i,
			),
		).toBeInTheDocument();
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
		await confirmRotate();
		expect(
			await screen.findByText(/state key rotated\. revoked 2 other session/i),
		).toBeInTheDocument();
	});
});
