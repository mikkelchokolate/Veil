import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SetupView } from "../auth/SetupView";
import { I18nProvider } from "../i18n/I18nContext";
import { HttpResponse, http, server } from "./server";

function renderSetup() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<I18nProvider>
				<SetupView />
			</I18nProvider>
		</QueryClientProvider>,
	);
}

describe("SetupView", () => {
	it("requires backup acknowledgement and posts backupAcknowledged", async () => {
		const user = userEvent.setup();
		let body: Record<string, unknown> | null = null;
		server.use(
			http.post("/api/setup/complete", async ({ request }) => {
				body = (await request.json()) as Record<string, unknown>;
				return HttpResponse.json({ completed: true }, { status: 201 });
			}),
		);
		renderSetup();
		await user.clear(screen.getByLabelText("Username"));
		await user.type(screen.getByLabelText("Username"), "admin");
		await user.type(
			screen.getByLabelText("Password"),
			"a-long-secure-password",
		);
		await user.type(
			screen.getByLabelText("Confirm password"),
			"a-long-secure-password",
		);
		await user.click(
			screen.getByRole("checkbox", {
				name: /preserve both the encrypted state/i,
			}),
		);
		await user.click(
			screen.getByRole("button", { name: "Create administrator" }),
		);
		await waitFor(() => expect(body).not.toBeNull());
		expect(body).toMatchObject({
			username: "admin",
			password: "a-long-secure-password",
			backupAcknowledged: true,
		});
	});
});
