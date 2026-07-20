import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "./api/fetcher";
import { AuthProvider, useAuth } from "./auth/AuthContext";
import { LoginView } from "./auth/LoginView";
import { SetupView } from "./auth/SetupView";
import { I18nProvider } from "./i18n/I18nContext";
import { RouterView } from "./router";

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

function Gate() {
	const setup = useSetupStatus();
	const { session, loading } = useAuth();

	if (setup.isLoading || loading) {
		return (
			<div className="center-screen">
				<span className="spinner" aria-label="loading" />
			</div>
		);
	}

	if (setup.data?.required && setup.data.allowed) {
		return <SetupView />;
	}

	if (!session?.authenticated) {
		return <LoginView />;
	}

	return (
		<I18nProvider
			initialLocale={(session?.locale as "en" | "ru" | undefined) ?? "en"}
		>
			<RouterView />
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
