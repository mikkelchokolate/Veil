import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

type JSONRecord = Record<string, unknown>;

const durableFields = [
	"id",
	"name",
	"email",
	"enabled",
	"groupId",
	"quotaBytes",
	"quotaResetPolicy",
	"quotaResetAt",
	"expiresAt",
	"deviceLimit",
	"notes",
	"depleted",
	"createdAt",
] as const;

function fullyPopulatedClient(): JSONRecord {
	return {
		id: "client-full",
		name: "Fully Populated",
		email: "owner@example.test",
		enabled: true,
		groupId: "production-group",
		quotaBytes: 987654321,
		quotaResetPolicy: "weekly",
		quotaResetAt: 1893456000,
		expiresAt: 1924992000,
		deviceLimit: 7,
		notes: "durable notes",
		depleted: false,
		createdAt: 1700000000,
		updatedAt: 1700000000,
		version: 1,
		status: "active",
		inboundIds: ["edge"],
		hasCredentials: true,
		bindings: [
			{
				id: "binding-1",
				clientId: "client-full",
				inboundId: "edge",
				enabled: true,
				version: 1,
				credential: { configured: true, kind: "password", version: 1 },
			},
		],
	};
}

function clone<T>(value: T): T {
	return JSON.parse(JSON.stringify(value)) as T;
}

const uiLoadTimeout = { timeout: 5_000 };

function renderClientDetail() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/clients/client-full"] }),
	});
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<RouterProvider router={router} />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

function installStatefulClientAPI() {
	let client = fullyPopulatedClient();
	const initial = clone(client);
	const requests: Array<{ method: string; path: string; body: JSONRecord }> =
		[];

	const respond = () => HttpResponse.json(clone(client));
	server.use(
		http.get("/api/v1/clients/client-full", respond),
		http.get("/api/inbounds", () =>
			HttpResponse.json([
				{ name: "edge", protocol: "hysteria2", enabled: true },
				{ name: "spare", protocol: "mieru", enabled: true },
			]),
		),
		http.all("/api/v1/clients/client-full", async ({ request }) => {
			if (request.method === "GET") return respond();
			const body = (await request.json()) as JSONRecord;
			requests.push({
				method: request.method,
				path: "/client",
				body: clone(body),
			});
			if (request.method === "PATCH") {
				for (const field of durableFields) {
					if (
						field in body &&
						field !== "id" &&
						field !== "createdAt" &&
						field !== "depleted"
					) {
						client[field] = body[field];
					}
				}
			} else if (request.method === "PUT") {
				// Model the pre-remediation server's destructive replacement semantics:
				// omitted optional fields become nil/zero values.
				client = {
					...client,
					name: body.name ?? "",
					email: body.email ?? null,
					enabled: body.enabled ?? false,
					groupId: body.groupId ?? null,
					quotaBytes: body.quotaBytes ?? null,
					quotaResetPolicy: body.quotaResetPolicy ?? "",
					quotaResetAt: body.quotaResetAt ?? null,
					expiresAt: body.expiresAt ?? null,
					deviceLimit: body.deviceLimit ?? null,
					notes: body.notes ?? "",
				};
			}
			client.version = Number(client.version) + 1;
			client.updatedAt = Number(client.updatedAt) + 1;
			return respond();
		}),
		http.patch(
			"/api/v1/clients/client-full/bindings/binding-1",
			async ({ request }) => {
				const body = (await request.json()) as JSONRecord;
				requests.push({
					method: request.method,
					path: "/binding",
					body: clone(body),
				});
				const bindings = client.bindings as JSONRecord[];
				bindings[0] = {
					...bindings[0],
					enabled: body.enabled,
					version: Number(bindings[0].version) + 1,
				};
				return HttpResponse.json(bindings[0]);
			},
		),
		http.post(
			"/api/v1/clients/client-full/credentials/binding-1/rotate",
			async ({ request }) => {
				requests.push({
					method: request.method,
					path: "/credential/rotate",
					body: {},
				});
				return HttpResponse.json({
					plaintext: "new-one-time-secret",
					success: true,
				});
			},
		),
		http.post("/api/v1/clients/client-full/bindings", async ({ request }) => {
			const body = (await request.json()) as JSONRecord;
			requests.push({
				method: request.method,
				path: "/bindings",
				body: clone(body),
			});
			const binding = {
				id: "binding-2",
				clientId: "client-full",
				inboundId: body.inboundId,
				enabled: true,
				version: 1,
				credential: { configured: true, kind: "password", version: 1 },
			};
			(client.bindings as JSONRecord[]).push(binding);
			return HttpResponse.json(
				{
					...binding,
					plaintext: "attach-one-time-secret",
					success: true,
				},
				{ status: 201 },
			);
		}),
		http.delete(
			"/api/v1/clients/client-full/bindings/binding-1",
			({ request }) => {
				requests.push({
					method: request.method,
					path: "/binding/delete",
					body: {},
				});
				client.bindings = (client.bindings as JSONRecord[]).filter(
					(binding) => binding.id !== "binding-1",
				);
				return HttpResponse.json({ id: "binding-1" });
			},
		),
	);

	return {
		initial,
		requests,
		current: () => clone(client),
	};
}

function expectDurableRecordUnchanged(
	before: JSONRecord,
	after: JSONRecord,
	changes: Partial<JSONRecord> = {},
) {
	for (const field of durableFields) {
		const expected = field in changes ? changes[field] : before[field];
		expect(after[field], `durable field ${field}`).toEqual(expected);
	}
}

describe("ClientDetailPage lossless UI mutations", () => {
	it("edits a fully populated client with PATCH and preserves every omitted durable field", async () => {
		const api = installStatefulClientAPI();
		const user = userEvent.setup();
		renderClientDetail();
		const name = await screen.findByLabelText(/^name$/i, {}, uiLoadTimeout);
		await user.clear(name);
		await user.type(name, "Renamed Only");
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		await waitFor(() =>
			expect(api.requests.some((r) => r.path === "/client")).toBe(true),
		);

		const request = api.requests.find((r) => r.path === "/client");
		expect(request?.method).toBe("PATCH");
		expect(request?.body).toEqual({ version: 1, name: "Renamed Only" });
		expectDurableRecordUnchanged(api.initial, api.current(), {
			name: "Renamed Only",
		});
	});

	it("sends explicit null when nullable form fields are cleared", async () => {
		const api = installStatefulClientAPI();
		const user = userEvent.setup();
		renderClientDetail();
		for (const label of [/email/i, /quota/i, /expiry date/i]) {
			const input = await screen.findByLabelText(label, {}, uiLoadTimeout);
			await user.clear(input);
		}
		await user.clear(screen.getByLabelText(/notes/i));
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		await waitFor(() =>
			expect(api.requests.some((r) => r.path === "/client")).toBe(true),
		);
		const body = api.requests.find((r) => r.path === "/client")?.body;
		expect(body).toMatchObject({
			version: 1,
			email: null,
			quotaBytes: null,
			expiresAt: null,
			notes: null,
		});
	});

	it("enable/disable changes only enabled on a fully populated record", async () => {
		const api = installStatefulClientAPI();
		const user = userEvent.setup();
		renderClientDetail();
		await user.click(
			await screen.findByRole(
				"button",
				{ name: /disable client/i },
				uiLoadTimeout,
			),
		);
		await waitFor(() =>
			expect(api.requests.some((r) => r.path === "/client")).toBe(true),
		);
		const request = api.requests.find((r) => r.path === "/client");
		expect(request?.method).toBe("PATCH");
		expect(request?.body).toEqual({ version: 1, enabled: false });
		expectDurableRecordUnchanged(api.initial, api.current(), {
			enabled: false,
		});
	});

	it("binding toggle, credential rotation, attach and detach never rewrite Client fields", async () => {
		const api = installStatefulClientAPI();
		const user = userEvent.setup();
		renderClientDetail();
		await user.click(
			await screen.findByRole("tab", { name: /access/i }, uiLoadTimeout),
		);
		const edge = await screen.findByText("edge", {}, uiLoadTimeout);
		const card = edge.parentElement?.parentElement;
		if (!card) throw new Error("binding card unavailable");
		await user.click(within(card).getByRole("button", { name: /^disable$/i }));
		await waitFor(() =>
			expect(api.requests.some((r) => r.path === "/binding")).toBe(true),
		);
		await user.click(
			within(card).getByRole("button", { name: /rotate credential/i }),
		);
		await waitFor(() =>
			expect(api.requests.some((r) => r.path === "/credential/rotate")).toBe(
				true,
			),
		);
		expect(await screen.findByText("new-one-time-secret")).toBeInTheDocument();
		expect(screen.queryByText(/rotate failed/i)).not.toBeInTheDocument();
		await user.selectOptions(screen.getByLabelText(/attach inbound/i), "spare");
		await user.click(screen.getByRole("button", { name: /^attach$/i }));
		await waitFor(() =>
			expect(api.requests.some((r) => r.path === "/bindings")).toBe(true),
		);
		expect(await screen.findByText("attach-one-time-secret")).toBeInTheDocument();
		await user.click(within(card).getByRole("button", { name: /detach/i }));
		await waitFor(() =>
			expect(api.requests.some((r) => r.path === "/binding/delete")).toBe(true),
		);

		expectDurableRecordUnchanged(api.initial, api.current());
		expect(
			api.requests.filter((request) => request.path === "/client"),
		).toHaveLength(0);
	});
});
