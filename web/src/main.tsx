import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";

const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			retry: 1,
			refetchOnWindowFocus: false,
			staleTime: 5_000,
		},
	},
});

declare global {
	interface Window {
		__VEIL_READY?: boolean;
	}
}

const rootEl = document.getElementById("root");
if (!rootEl) {
	throw new Error("missing #root");
}

createRoot(rootEl).render(
	<StrictMode>
		<QueryClientProvider client={queryClient}>
			<App />
		</QueryClientProvider>
	</StrictMode>,
);

window.__VEIL_READY = true;
