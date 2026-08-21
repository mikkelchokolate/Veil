import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "../i18n/I18nContext";
import { RevealLink } from "../subscription/RevealLink";

describe("RevealLink", () => {
	it("keeps the URL and QR hidden until the operator asks to see them", async () => {
		const user = userEvent.setup();
		const uri = "https://vpn.example.com/s/example-token";
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<I18nProvider>
					<RevealLink
						value={uri}
						copied={false}
						onCopy={() => undefined}
						showLabel="Show link"
						hideLabel="Hide link"
						copyLabel="Copy"
						copiedLabel="Copied"
						urlLabel="Subscription URL"
					/>
				</I18nProvider>
			</QueryClientProvider>,
		);

		expect(
			screen.getByRole("button", { name: "Show link" }),
		).toBeInTheDocument();
		expect(screen.queryByText(uri)).not.toBeInTheDocument();
		expect(
			screen.queryByRole("img", { name: "subscription QR code" }),
		).not.toBeInTheDocument();
		expect(
			screen.queryByLabelText("subscription QR code"),
		).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Hide link" }),
		).not.toBeInTheDocument();

		await user.click(screen.getByRole("button", { name: "Show link" }));

		expect(screen.getByText(uri)).toBeInTheDocument();
		expect(screen.getByLabelText("subscription QR code")).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Hide link" }),
		).toBeInTheDocument();

		await user.click(screen.getByRole("button", { name: "Hide link" }));

		expect(screen.queryByText(uri)).not.toBeInTheDocument();
		expect(
			screen.queryByLabelText("subscription QR code"),
		).not.toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Show link" }),
		).toBeInTheDocument();
	});
});
