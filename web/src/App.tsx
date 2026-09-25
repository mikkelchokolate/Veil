import { useQuery } from "@tanstack/react-query";
import { lazy, Suspense } from "react";
import { ApiError, apiFetch } from "./api/fetcher";
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

function SetupStatusError({ error }: { error: unknown }) {
	const { t } = useI18n();
	return (
		<div className="center-screen">
			<p className="form-error" role="alert">
				{error instanceof ApiError
					? error.message
					: t("auth.setup.unavailable")}
			</p>
		</div>
	);
}

function Gate() {
	const setup = useSetupStatus();
	const { session } = useAuth();

	// I18nProvider wraps every branch (login/setup views also use t()). It is
	// intentionally NOT keyed on the session locale: a key would remount the
	// provider and with it the whole router subtree, silently discarding open
	// dialogs and unsaved form state when the delayed auth refresh lands
	// (#1044). The provider syncs prop changes itself via useEffect.
	return (
		<I18nProvider
			initialLocale={(session?.locale as "en" | "ru" | undefined) ?? "en"}
		>
			{setup.data?.required && setup.data.allowed ? (
				<SetupView />
			) : setup.isError ? (
				<SetupStatusError error={setup.error} />
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
