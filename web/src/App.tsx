import { useQuery } from "@tanstack/react-query";
import { lazy, Suspense } from "react";
import { apiFetch } from "./api/fetcher";
import { AuthProvider, useAuth } from "./auth/AuthContext";
import { LoginView } from "./auth/LoginView";
import { SetupView } from "./auth/SetupView";
import { I18nProvider, useI18n } from "./i18n/I18nContext";

const RouterView = lazy(async () => {
	const mod = await import("./router");
	return { default: mod.RouterView };
});

interface SetupStatus {
	required: boolean;
	allowed: boolean;
}

function useSetupStatus() {
	return useQuery<SetupStatus>({
		queryKey: ["setup", "status"],
		queryFn: () => apiFetch<SetupStatus>("/api/setup/status"),
		staleTime: 30_000,
		retry: false,
	});
}

function BusySpinner() {
	const { t } = useI18n();
	return (
		<div className="center-screen">
			<span
				className="spinner"
				role="status"
				aria-label={t("common.loading")}
			/>
		</div>
	);
}

function Gate() {
	const setup = useSetupStatus();
	const { session } = useAuth();

	// I18nProvider wraps every branch (login/setup views also use t()). The key
	// remounts the provider when the authenticated locale becomes known or the
	// user switches language, so initialLocale always matches the session.
	return (
		<I18nProvider
			key={session?.locale ?? "anon"}
			initialLocale={(session?.locale as "en" | "ru" | undefined) ?? "en"}
		>
			{setup.data?.required && setup.data.allowed ? (
				<SetupView />
			) : session?.authenticated ? (
				<Suspense fallback={<BusySpinner />}>
					<RouterView />
				</Suspense>
			) : (
				<LoginView />
			)}
		</I18nProvider>
	);
}

export function App() {
	return (
		<AuthProvider>
			<Gate />
		</AuthProvider>
	);
}
