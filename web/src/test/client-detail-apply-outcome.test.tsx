import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

function renderClientDetail() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/clients/c1"] }),
	});
	const view = render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<RouterProvider router={router} />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
	return { ...view, router };
}

const CLIENT = {
	id: "c1",
	name: "Alice",
	enabled: true,
	version: 1,
	status: "active",
	bindings: [
		{
			id: "b1",
			inboundId: "hy2-a",
			runtimeIdentity: "alice-hy2-a",
			enabled: true,
			version: 1,
			capability: { protocol: "hysteria2" },
		},
	],
};

const INBOUNDS = [
	{ name: "hy2-a", protocol: "hysteria2", enabled: true },
	{ name: "hy2-b", protocol: "hysteria2", enabled: true },
];

const failedOutcome = {
	success: false,
	revision: { desired: 2, applied: 1, state: "failed" },
	applyJob: {
		id: "job-fail",
		desiredRevision: 2,
		baseRevision: 1,
		status: "failed",
		trigger: "mutation",
		createdAt: 1700000000,
	},
};

function baseHandlers() {
	return [
		http.get("/api/v1/clients/c1", () => HttpResponse.json(CLIENT)),
		http.get("/api/v1/clients", () =>
			HttpResponse.json({
				items: [CLIENT],
				total: 1,
				page: 1,
				pageSize: 25,
			}),
		),
		http.get("/api/inbounds", () => HttpResponse.json(INBOUNDS)),
	];
}

async function expectApplyFailedBadge() {
	expect(await screen.findByText(/^apply failed$/i)).toBeInTheDocument();
	// The clean "saved" badge must not render next to the failure.
	expect(screen.queryByText(/^saved$/i)).not.toBeInTheDocument();
}

// #676: every ClientDetail mutation envelope can commit while the auto-apply
// fails (success=false on 200) — the page must show the apply-failed badge
// via recordFeedback instead of a green "saved". The delete twin already has
// coverage in client-detail-delete.test.tsx.
describe("ClientDetailPage mutation apply outcome", () => {
	it("shows the apply-failed badge when a save commits but apply fails", async () => {
		const user = userEvent.setup();
		server.use(
			...baseHandlers(),
			http.patch("/api/v1/clients/c1", () =>
				HttpResponse.json({ ...CLIENT, ...failedOutcome }),
			),
		);
		renderClientDetail();
		await screen.findByText("Alice");
		const name = await screen.findByLabelText(/^name$/i);
		await user.clear(name);
		await user.type(name, "Alice2");
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		await expectApplyFailedBadge();
	});

	it("shows the apply-failed badge when an attach commits but apply fails", async () => {
		const user = userEvent.setup();
		server.use(
			...baseHandlers(),
			http.post("/api/v1/clients/c1/bindings", () =>
				HttpResponse.json({
					id: "b2",
					inboundId: "hy2-b",
					runtimeIdentity: "alice-hy2-b",
					enabled: true,
					version: 1,
					...failedOutcome,
				}),
			),
		);
		renderClientDetail();
		await screen.findByText("Alice");
		await user.click(screen.getByRole("tab", { name: /^access$/i }));
		await user.selectOptions(
			await screen.findByLabelText(/attach inbound/i),
			"hy2-b",
		);
		await user.click(screen.getByRole("button", { name: /^attach$/i }));
		await expectApplyFailedBadge();
	});

	it("shows the apply-failed badge when a credential rotate commits but apply fails", async () => {
		const user = userEvent.setup();
		server.use(
			...baseHandlers(),
			http.post("/api/v1/clients/c1/credentials/b1/rotate", () =>
				HttpResponse.json({ ...failedOutcome }),
			),
		);
		renderClientDetail();
		await screen.findByText("Alice");
		await user.click(screen.getByRole("tab", { name: /^access$/i }));
		await user.click(
			screen.getByRole("button", { name: /rotate credential/i }),
		);
		await expectApplyFailedBadge();
	});

	it("shows the apply-failed badge when a detach commits but apply fails", async () => {
		const user = userEvent.setup();
		server.use(
			...baseHandlers(),
			http.delete("/api/v1/clients/c1/bindings/b1", () =>
				HttpResponse.json({ ...failedOutcome }),
			),
		);
		renderClientDetail();
		await screen.findByText("Alice");
		await user.click(screen.getByRole("tab", { name: /^access$/i }));
		await user.click(screen.getByRole("button", { name: /^detach$/i }));
		await expectApplyFailedBadge();
	});
});
