import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AuthProvider, useAuth } from "../auth/AuthContext";
import { HttpResponse, http, server } from "./server";

class TestBroadcastChannel {
	static channels = new Map<string, Set<TestBroadcastChannel>>();
	onmessage: ((event: MessageEvent) => void) | null = null;
	readonly name: string;

	constructor(name: string) {
		this.name = name;
		const channels = TestBroadcastChannel.channels.get(name) ?? new Set();
		channels.add(this);
		TestBroadcastChannel.channels.set(name, channels);
	}

	postMessage(data: unknown) {
		for (const channel of TestBroadcastChannel.channels.get(this.name) ?? []) {
			if (channel !== this)
				queueMicrotask(() => channel.onmessage?.({ data } as MessageEvent));
		}
	}

	close() {
		TestBroadcastChannel.channels.get(this.name)?.delete(this);
	}

	addEventListener(type: string, listener: (event: MessageEvent) => void) {
		if (type === "message") this.onmessage = listener;
	}

	removeEventListener(type: string, listener: (event: MessageEvent) => void) {
		if (type === "message" && this.onmessage === listener)
			this.onmessage = null;
	}
}

function Probe({ id }: { id: string }) {
	const auth = useAuth();
	const session = auth.session;
	return (
		<div>
			<output data-testid={`${id}-session`}>
				{JSON.stringify({
					authenticated: session?.authenticated,
					role: session?.role,
					locale: session?.locale,
					csrfToken: session?.csrfToken,
				})}
			</output>
			<button type="button" onClick={() => void auth.logout()}>
				logout-{id}
			</button>
		</div>
	);
}

function Tab({ children }: { children: ReactNode }) {
	return <AuthProvider>{children}</AuthProvider>;
}

describe("cross-tab auth synchronization", () => {
	beforeEach(() => {
		TestBroadcastChannel.channels.clear();
		vi.stubGlobal("BroadcastChannel", TestBroadcastChannel);
	});

	it("propagates logout and refreshes role/locale/CSRF on focus", async () => {
		let session = {
			authenticated: true,
			username: "admin",
			role: "admin",
			locale: "en",
			csrfToken: "csrf-one",
		};
		server.use(
			http.get("*/api/auth/status", () => HttpResponse.json(session)),
			http.post("*/api/auth/logout", () => {
				session = { ...session, authenticated: false };
				return new HttpResponse(null, { status: 204 });
			}),
		);
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<Tab>
					<Probe id="tab-one" />
				</Tab>
				<Tab>
					<Probe id="tab-two" />
				</Tab>
			</QueryClientProvider>,
		);
		await waitFor(() =>
			expect(screen.getByTestId("tab-one-session")).toHaveTextContent(
				'"role":"admin"',
			),
		);
		await waitFor(() =>
			expect(screen.getByTestId("tab-two-session")).toHaveTextContent(
				'"role":"admin"',
			),
		);

		await userEvent.click(
			screen.getByRole("button", { name: "logout-tab-one" }),
		);
		await waitFor(() =>
			expect(screen.getByTestId("tab-one-session")).toHaveTextContent(
				'"authenticated":false',
			),
		);
		await waitFor(() =>
			expect(screen.getByTestId("tab-two-session")).toHaveTextContent(
				'"authenticated":false',
			),
		);

		session = {
			authenticated: true,
			username: "viewer",
			role: "viewer",
			locale: "ru",
			csrfToken: "csrf-two",
		};
		window.dispatchEvent(new Event("focus"));
		document.dispatchEvent(new Event("visibilitychange"));
		for (const id of ["tab-one", "tab-two"]) {
			await waitFor(() => {
				const output = screen.getByTestId(`${id}-session`);
				expect(output).toHaveTextContent('"role":"viewer"');
				expect(output).toHaveTextContent('"locale":"ru"');
				expect(output).toHaveTextContent('"csrfToken":"csrf-two"');
			});
		}
	});
});
