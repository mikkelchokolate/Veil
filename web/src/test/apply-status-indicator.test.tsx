import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
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
