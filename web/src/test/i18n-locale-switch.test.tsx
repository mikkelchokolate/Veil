import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { I18nProvider, useI18n } from "../i18n/I18nContext";
import { HttpResponse, http, server } from "./server";

// #1044: a locale refresh must re-render translations WITHOUT remounting the
// provider subtree — previously App keyed I18nProvider on the session locale,
// so the auth refresh landing (or a language switch) remounted the router and
// discarded open dialogs and unsaved form state.

function Probe() {
	const { t, locale, setLocale } = useI18n();
	const [draft, setDraft] = useState("");
	return (
		<div>
			<input
				aria-label="draft"
				value={draft}
				onChange={(e) => setDraft(e.target.value)}
			/>
			<p data-testid="loading-label">{t("common.loading")}</p>
			<p data-testid="locale">{locale}</p>
			<button type="button" onClick={() => setLocale("ru")}>
				switch-to-ru
			</button>
		</div>
	);
}

function renderProvider(initialLocale: "en" | "ru" = "en") {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={qc}>
			<I18nProvider initialLocale={initialLocale}>
				<Probe />
			</I18nProvider>
		</QueryClientProvider>,
	);
}

describe("I18nProvider locale switching (#1044)", () => {
	it("syncs a changed initialLocale without remounting children", async () => {
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		const view = render(
			<QueryClientProvider client={qc}>
				<I18nProvider initialLocale="en">
					<Probe />
				</I18nProvider>
			</QueryClientProvider>,
		);
		const input = screen.getByLabelText("draft");
		fireEvent.change(input, { target: { value: "unsaved draft" } });
		expect(screen.getByTestId("loading-label")).toHaveTextContent("Loading…");
		expect(screen.getByTestId("locale")).toHaveTextContent("en");

		// The session refresh lands a new initialLocale — same provider, same
		// subtree: the draft input must keep its value and DOM identity.
		view.rerender(
			<QueryClientProvider client={qc}>
				<I18nProvider initialLocale="ru">
					<Probe />
				</I18nProvider>
			</QueryClientProvider>,
		);
		await waitFor(() =>
			expect(screen.getByTestId("loading-label")).toHaveTextContent(
				"Загрузка…",
			),
		);
		expect(screen.getByTestId("locale")).toHaveTextContent("ru");
		expect(screen.getByLabelText("draft")).toHaveValue("unsaved draft");
		expect(screen.getByLabelText("draft")).toBe(input);
	});

	it("switches locale via setLocale without remounting children", async () => {
		server.use(
			http.post("/api/auth/locale", () => HttpResponse.json({ ok: true })),
		);
		renderProvider("en");
		const input = screen.getByLabelText("draft");
		fireEvent.change(input, { target: { value: "keep me" } });
		fireEvent.click(screen.getByRole("button", { name: "switch-to-ru" }));
		await waitFor(() =>
			expect(screen.getByTestId("locale")).toHaveTextContent("ru"),
		);
		expect(screen.getByTestId("loading-label")).toHaveTextContent("Загрузка…");
		expect(screen.getByLabelText("draft")).toHaveValue("keep me");
		expect(screen.getByLabelText("draft")).toBe(input);
	});
});
