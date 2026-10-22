import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { ApplyStatusIndicator } from "../apply/ApplyStatusIndicator";
import { I18nProvider } from "../i18n/I18nContext";
import { HttpResponse, http, server } from "./server";

function renderIndicator(locale: "en" | "ru" = "en") {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<I18nProvider initialLocale={locale}>
				<ApplyStatusIndicator />
			</I18nProvider>
		</QueryClientProvider>,
	);
}

// #703: the shell indicator's tooltip and drift suffix are localized — no
// hardcoded English "rev" copy in a non-English UI.
describe("ApplyStatusIndicator", () => {
	afterEach(() => vi.restoreAllMocks());
	it("keeps elapsed time and server update age moving when progress is unchanged", async () => {
		const clock = vi.spyOn(Date, "now").mockReturnValue(1_000_000);
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 1,
					state: "applying",
					progress: {
						stage: "building",
						runtime: "olcrtc",
						startedAt: 1000,
						updatedAt: 1000,
					},
				}),
			),
		);
		renderIndicator();
		await screen.findByText("Elapsed 0:00 · Stage updated 0s ago");
		clock.mockReturnValue(1_010_000);
		expect(
			await screen.findByText(
				"Elapsed 0:10 · Stage updated 10s ago",
				{},
				{ timeout: 2000 },
			),
		).toBeInTheDocument();
	});

	it("shows real installation stage and server update age in Russian", async () => {
		const now = Math.floor(Date.now() / 1000);
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 1,
					state: "applying",
					progress: {
						stage: "building",
						runtime: "olcrtc",
						startedAt: now - 72,
						updatedAt: now - 5,
					},
				}),
			),
		);
		renderIndicator("ru");
		expect(await screen.findByRole("status")).toHaveTextContent(
			"Сборка компонента",
		);
		expect(screen.getByRole("status")).toHaveTextContent("olcrtc");
		expect(screen.getByText(/Прошло 1:1[23]/)).toHaveTextContent(
			/Этап обновлён [56] с назад/,
		);
		expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
	});

	it("uses a localized generic label for an unknown progress stage", async () => {
		const now = Math.floor(Date.now() / 1000);
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 1,
					state: "applying",
					progress: { stage: "future_stage", startedAt: now, updatedAt: now },
				}),
			),
		);
		renderIndicator();
		expect(await screen.findByRole("status")).toHaveTextContent(
			"Applying configuration",
		);
		expect(
			screen.queryByText(/future_stage|applyProgress\./),
		).not.toBeInTheDocument();
	});

	it("renders a localized label plus drift tooltip and revision suffix", async () => {
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 3,
					appliedRevision: 1,
					state: "pending",
				}),
			),
		);
		renderIndicator();
		const badge = await screen.findByText(/pending/i);
		expect(badge).toHaveAttribute("title", "desired rev 3, runtime rev 1");
		expect(badge.textContent).toContain("rev 1→3");
	});

	it("localizes the tooltip and suffix in ru", async () => {
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 3,
					appliedRevision: 1,
					state: "degraded",
				}),
			),
		);
		renderIndicator("ru");
		const badge = await screen.findByText(/Деградировано/);
		expect(badge).toHaveAttribute(
			"title",
			"целевая ревизия 3, текущая ревизия 1",
		);
		expect(badge.textContent).toContain("рев. 1→3");
		expect(badge.className).toContain("badge-danger");
	});

	it("shows a plain runtime tooltip when revisions match", async () => {
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 2,
					state: "synced",
				}),
			),
		);
		renderIndicator();
		const badge = await screen.findByText("Synced");
		expect(badge).toHaveAttribute("title", "runtime rev 2");
		expect(badge.className).toContain("badge-success");
	});
});
