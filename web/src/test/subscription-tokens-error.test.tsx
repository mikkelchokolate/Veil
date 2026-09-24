import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import { I18nProvider } from "../i18n/I18nContext";
import { SubscriptionTokensPanel } from "../subscription/SubscriptionTokensPanel";
import { HttpResponse, http, server } from "./server";

vi.mock("../auth/AuthContext", () => ({
	useIsAdmin: () => true,
}));

describe("SubscriptionTokensPanel errors", () => {
	it("does not treat a failed token list as empty", async () => {
		server.use(
			http.get("/api/v1/clients/c1/tokens", () =>
				HttpResponse.json({ error: { message: "forbidden" } }, { status: 403 }),
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<SubscriptionTokensPanel clientId="c1" />
				</I18nProvider>
			</QueryClientProvider>,
		);
		expect(await screen.findByText(/forbidden/i)).toBeInTheDocument();
		expect(
			screen.queryByText(/no subscription tokens/i),
		).not.toBeInTheDocument();
	});

	it("keeps the one-time rotate URL after the token list refetches", async () => {
		const user = userEvent.setup();
		let listGets = 0;
		server.use(
			http.get("/api/v1/clients/c1/tokens", () => {
				listGets += 1;
				return HttpResponse.json({
					items: [
						{
							id: "tok-1",
							// The refetched row only carries the new prefix — the rotated
							// URL is one-time and never comes back in a list payload.
							prefix: listGets === 1 ? "veil_ab" : "veil_cd",
							label: "phone",
							enabled: true,
							createdAt: 1700000000,
						},
					],
				});
			}),
			http.post("/api/v1/clients/c1/tokens/tok-1/rotate", () =>
				HttpResponse.json({
					token: {
						id: "tok-1",
						prefix: "veil_cd",
						label: "phone",
						enabled: true,
						createdAt: 1700000000,
					},
					plaintext: "veil_cd_secret",
					url: "/s/veil_cd_secret",
				}),
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<SubscriptionTokensPanel clientId="c1" />
				</I18nProvider>
			</QueryClientProvider>,
		);
		await user.click(await screen.findByRole("button", { name: /^rotate$/i }));
		// #714: rotating a live token confirms before the POST fires.
		await user.click(
			await screen.findByRole("button", { name: /confirm rotate/i }),
		);
		expect(
			await screen.findByTestId("issued-subscription-token"),
		).toBeInTheDocument();
		expect(screen.getByText(/new token \(shown once\)/i)).toBeInTheDocument();
		// #832: wait for the post-rotate list refetch to land — the one-time
		// banner must still carry the URL after the list repaints.
		await waitFor(() => expect(listGets).toBeGreaterThan(1));
		const banner = await screen.findByTestId("issued-subscription-token");
		await user.click(
			within(banner).getByRole("button", { name: /show link/i }),
		);
		expect(
			await within(banner).findByText(/\/s\/veil_cd_secret/),
		).toBeInTheDocument();
	});

	// #699: revoke permanently kills the subscription URL — it must confirm
	// before the DELETE, like the expired-rotate and other Panel gates.
	it("does not DELETE a token until revoke is confirmed", async () => {
		const user = userEvent.setup();
		const deletes: string[] = [];
		server.use(
			http.get("/api/v1/clients/c1/tokens", () =>
				HttpResponse.json({
					items: [
						{
							id: "tok-1",
							prefix: "veil_ab",
							label: "phone",
							enabled: true,
							createdAt: 1700000000,
						},
					],
				}),
			),
			http.delete("/api/v1/clients/c1/tokens/tok-1", () => {
				deletes.push("tok-1");
				return HttpResponse.json({ id: "tok-1" });
			}),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<SubscriptionTokensPanel clientId="c1" />
				</I18nProvider>
			</QueryClientProvider>,
		);
		await user.click(await screen.findByRole("button", { name: /^revoke$/i }));
		expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
		expect(deletes).toEqual([]);
		await user.click(screen.getByRole("button", { name: /confirm revoke/i }));
		await waitFor(() => expect(deletes).toEqual(["tok-1"]));
	});

	// #714: rotating an ACTIVE token confirms too — only the expired path
	// had a dialog before.
	it("does not POST rotate on an active token until confirmed", async () => {
		const user = userEvent.setup();
		const rotates: unknown[] = [];
		server.use(
			http.get("/api/v1/clients/c1/tokens", () =>
				HttpResponse.json({
					items: [
						{
							id: "tok-1",
							prefix: "veil_ab",
							label: "phone",
							enabled: true,
							createdAt: 1700000000,
						},
					],
				}),
			),
			http.post("/api/v1/clients/c1/tokens/tok-1/rotate", () => {
				rotates.push("rotate");
				return HttpResponse.json({
					token: {
						id: "tok-1",
						prefix: "veil_cd",
						label: "phone",
						enabled: true,
						createdAt: 1700000000,
					},
					plaintext: "veil_cd_secret",
					url: "/s/veil_cd_secret",
				});
			}),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<SubscriptionTokensPanel clientId="c1" />
				</I18nProvider>
			</QueryClientProvider>,
		);
		await user.click(await screen.findByRole("button", { name: /^rotate$/i }));
		expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
		expect(rotates).toEqual([]);
		await user.click(screen.getByRole("button", { name: /confirm rotate/i }));
		await waitFor(() => expect(rotates).toHaveLength(1));
	});

	// #725: the backend still returns stored URLs for expired/disabled tokens,
	// but those URLs no longer authenticate — never render them as copyable.
	it("hides the copyable URL on an expired token", async () => {
		const expiredAt = Math.floor(Date.now() / 1000) - 60;
		server.use(
			http.get("/api/v1/clients/c1/tokens", () =>
				HttpResponse.json({
					items: [
						{
							id: "tok-exp",
							prefix: "veil_ex",
							label: "old phone",
							enabled: true,
							createdAt: 1700000000,
							expiresAt: expiredAt,
							url: "/s/veil_ex_secret",
						},
					],
				}),
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<SubscriptionTokensPanel clientId="c1" />
				</I18nProvider>
			</QueryClientProvider>,
		);
		expect(await screen.findByText(/^expired$/i)).toBeInTheDocument();
		expect(screen.getByText(/no longer authenticates/i)).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /show link|copy url/i }),
		).not.toBeInTheDocument();
		expect(screen.queryByText(/veil_ex_secret/)).not.toBeInTheDocument();
	});

	it("hides the copyable URL on a disabled token", async () => {
		server.use(
			http.get("/api/v1/clients/c1/tokens", () =>
				HttpResponse.json({
					items: [
						{
							id: "tok-dis",
							prefix: "veil_di",
							label: "paused",
							enabled: false,
							createdAt: 1700000000,
							url: "/s/veil_di_secret",
						},
					],
				}),
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<SubscriptionTokensPanel clientId="c1" />
				</I18nProvider>
			</QueryClientProvider>,
		);
		expect(await screen.findByText(/^disabled$/i)).toBeInTheDocument();
		expect(screen.getByText(/does not authenticate/i)).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /show link|copy url/i }),
		).not.toBeInTheDocument();
		expect(screen.queryByText(/veil_di_secret/)).not.toBeInTheDocument();
	});

	it("shows an expired token as expired and requires a future expiry to rotate", async () => {
		const user = userEvent.setup();
		const rotateBodies: unknown[] = [];
		const expiredAt = Math.floor(Date.now() / 1000) - 60;
		server.use(
			http.get("/api/v1/clients/c1/tokens", () =>
				HttpResponse.json({
					items: [
						{
							id: "tok-exp",
							prefix: "veil_ex",
							label: "old phone",
							enabled: true,
							createdAt: 1700000000,
							expiresAt: expiredAt,
						},
					],
				}),
			),
			http.post(
				"/api/v1/clients/c1/tokens/tok-exp/rotate",
				async ({ request }) => {
					const text = await request.text();
					rotateBodies.push(text ? JSON.parse(text) : undefined);
					return HttpResponse.json({
						token: {
							id: "tok-exp",
							prefix: "veil_nw",
							label: "old phone",
							enabled: true,
							createdAt: 1700000000,
						},
						plaintext: "veil_nw_secret",
						url: "/s/veil_nw_secret",
					});
				},
			),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<SubscriptionTokensPanel clientId="c1" />
				</I18nProvider>
			</QueryClientProvider>,
		);
		expect(await screen.findByText(/^expired$/i)).toBeInTheDocument();
		expect(screen.queryByText(/^active$/i)).not.toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /^rotate$/i }));
		expect(rotateBodies).toEqual([]);
		fireEvent.change(await screen.findByLabelText(/new expiry/i), {
			target: { value: "2099-01-01" },
		});
		await user.click(
			screen.getByRole("button", { name: /rotate with new expiry/i }),
		);
		await waitFor(() => expect(rotateBodies).toHaveLength(1));
		const body = rotateBodies[0] as { expiresAt?: number };
		expect(body.expiresAt).toBeGreaterThan(Date.now() / 1000);
	});
});
