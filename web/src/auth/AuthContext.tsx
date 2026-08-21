import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useRef,
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
	const channelRef = useRef<BroadcastChannel | null>(null);

	const broadcastRefresh = useCallback(() => {
		channelRef.current?.postMessage({ type: "refresh" });
		try {
			localStorage.setItem("veil-auth-refresh", String(Date.now()));
		} catch {
			// Storage can be disabled; BroadcastChannel/focus refresh still work.
		}
	}, []);

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

	useEffect(() => {
		const onRefresh = () => void refresh();
		let channel: BroadcastChannel | null = null;
		if (typeof BroadcastChannel !== "undefined") {
			channel = new BroadcastChannel("veil-auth");
			channel.onmessage = (event) => {
				if (event.data?.type === "refresh") onRefresh();
			};
			channelRef.current = channel;
		}
		const onStorage = (event: StorageEvent) => {
			if (event.key === "veil-auth-refresh") onRefresh();
		};
		const onFocus = () => onRefresh();
		const onVisibility = () => {
			if (document.visibilityState === "visible") onRefresh();
		};
		window.addEventListener("storage", onStorage);
		window.addEventListener("focus", onFocus);
		document.addEventListener("visibilitychange", onVisibility);
		return () => {
			window.removeEventListener("storage", onStorage);
			window.removeEventListener("focus", onFocus);
			document.removeEventListener("visibilitychange", onVisibility);
			channel?.close();
			if (channelRef.current === channel) channelRef.current = null;
		};
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
			try {
				localStorage.setItem("veil_spa", "1");
			} catch {
				/* storage may be disabled */
			}
			broadcastRefresh();
		},
		[refresh, broadcastRefresh],
	);

	const logout = useCallback(async () => {
		try {
			await apiFetch("/api/auth/logout", { method: "POST" });
		} finally {
			setSession({ authenticated: false });
			setCsrfToken(null);
			try {
				localStorage.removeItem("veil_spa");
			} catch {
				/* storage may be disabled */
			}
			broadcastRefresh();
		}
	}, [broadcastRefresh]);

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
