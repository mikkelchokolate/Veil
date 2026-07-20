import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useState,
} from "react";
import { apiFetch, setCsrfToken } from "../api/fetcher";
import type { UserRole } from "../api/generated/models";

export interface Session {
	authenticated: boolean;
	username?: string;
	role?: UserRole;
	locale?: string;
	csrfToken?: string;
}

interface AuthContextValue {
	session: Session | null;
	loading: boolean;
	login: (username: string, password: string) => Promise<void>;
	logout: () => Promise<void>;
	refresh: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
	const [session, setSession] = useState<Session | null>(null);
	const [loading, setLoading] = useState(true);

	const refresh = useCallback(async () => {
		try {
			// apiFetch returns the parsed body directly and throws on non-2xx.
			const status = await apiFetch<Session>("/api/auth/status");
			setSession(status);
			setCsrfToken(status.authenticated ? (status.csrfToken ?? null) : null);
		} catch {
			setSession({ authenticated: false });
			setCsrfToken(null);
		} finally {
			setLoading(false);
		}
	}, []);

	useEffect(() => {
		void refresh();
	}, [refresh]);

	const login = useCallback(
		async (username: string, password: string) => {
			const data = await apiFetch<{ csrfToken?: string }>("/api/auth/login", {
				method: "POST",
				body: JSON.stringify({ username, password }),
			});
			if (data?.csrfToken) {
				setCsrfToken(data.csrfToken);
			}
			await refresh();
		},
		[refresh],
	);

	const logout = useCallback(async () => {
		try {
			await apiFetch("/api/auth/logout", { method: "POST" });
		} finally {
			setSession({ authenticated: false });
			setCsrfToken(null);
		}
	}, []);

	const value = useMemo(
		() => ({ session, loading, login, logout, refresh }),
		[session, loading, login, logout, refresh],
	);

	return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
	const ctx = useContext(AuthContext);
	if (!ctx) {
		throw new Error("useAuth must be used within AuthProvider");
	}
	return ctx;
}

/** True when the session has admin (mutation) rights; viewer is read-only. */
export function useIsAdmin(): boolean {
	return useAuth().session?.role === "admin";
}
