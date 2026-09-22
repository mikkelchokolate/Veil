import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { WarpPage } from "../pages/WarpPage";
import { HttpResponse, http, server } from "./server";

function renderWarp() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<WarpPage />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

const snapshot = {
	enabled: true,
	endpoint: "engage.cloudflareclient.com:2408",
	privateKey: "[REDACTED]",
	licenseKey: "[REDACTED]",
	localAddress: "172.16.0.2/32",
	peerPublicKey: "peer-key",
	socksListen: "127.0.0.1",
	socksPort: 40001,
	mtu: 1280,
};

describe("WarpPage toggle", () => {
	it("echoes the redacted GET snapshot so enable/disable keeps the account", async () => {
		const puts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/warp", () => HttpResponse.json(snapshot)),
			http.put("/api/warp", async ({ request }) => {
				puts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ ...snapshot, enabled: false });
			}),
		);
		renderWarp();
		fireEvent.click(
			await screen.findByRole("button", { name: /disable warp/i }),
		);
		await waitFor(() => expect(puts).toHaveLength(1));
		expect(puts[0]).toMatchObject({
			enabled: false,
			privateKey: "[REDACTED]",
			licenseKey: "[REDACTED]",
			endpoint: snapshot.endpoint,
			localAddress: snapshot.localAddress,
			peerPublicKey: snapshot.peerPublicKey,
			socksPort: snapshot.socksPort,
			mtu: snapshot.mtu,
		});
		expect(puts[0]).not.toEqual({ enabled: false });
	});

	// #643: a 200 with success=false means the toggle committed but the
	// auto-apply failed — the page must say so, not silently repaint.
	it("surfaces a committed-but-unapplied toggle as an apply failure", async () => {
		server.use(
			http.get("/api/warp", () => HttpResponse.json(snapshot)),
			http.put("/api/warp", () =>
				HttpResponse.json({
					...snapshot,
					enabled: false,
					success: false,
					revision: { desired: 2, applied: 1, state: "failed" },
					applyJob: {
						id: "job-1",
						desiredRevision: 2,
						baseRevision: 1,
						status: "failed",
						trigger: "mutation",
						createdAt: 1700000000,
					},
				}),
			),
		);
		renderWarp();
		fireEvent.click(
			await screen.findByRole("button", { name: /disable warp/i }),
		);
		expect(
			await screen.findByText(/applying the change failed/i),
		).toBeInTheDocument();
	});
});
