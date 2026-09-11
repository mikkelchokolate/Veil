import { useQueryClient } from "@tanstack/react-query";
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
import {
	apiFetch,
	CancelledError,
	setCsrfToken,
	setUnauthorizedHandler,
} from "../api/fetcher";
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
	const qc = useQueryClient();
	const [session, setSession] = useState<Session | null>(null);
	const [loading, setLoading] = useState(true);
	const channelRef = useRef<BroadcastChannel | null>(null);
	const epochRef = useRef(0);
	const refreshSeqRef = useRef(0);
	const refreshAbortRef = useRef<AbortController | null>(null);
	const logoutInFlightRef = useRef(false);

	const abortRefresh = useCallback(() => {
		refreshAbortRef.current?.abort();
		refreshAbortRef.current = null;
	}, []);

	const broadcastRefresh = useCallback(() => {
		channelRef.current?.postMessage({ type: "refresh" });
		try {
			localStorage.setItem("veil-auth-refresh", String(Date.now()));
		} catch {
			// Storage can be disabled; BroadcastChannel/focus refresh still work.
		}
	}, []);

	const refresh = useCallback(async () => {
		if (logoutInFlightRef.current) return;
		const epoch = epochRef.current;
		const seq = ++refreshSeqRef.current;
		abortRefresh();
		const controller = new AbortController();
		refreshAbortRef.current = controller;
		try {
			// apiFetch returns the parsed body directly and throws on non-2xx.
			const status = await apiFetch<Session>("/api/auth/status", {
				signal: controller.signal,
			});
			if (
				logoutInFlightRef.current ||
				epoch !== epochRef.current ||
				seq !== refreshSeqRef.current
			) {
				return;
			}
			setSession(status);
			setCsrfToken(status.authenticated ? (status.csrfToken ?? null) : null);
		} catch (error) {
			if (
				logoutInFlightRef.current ||
				epoch !== epochRef.current ||
				seq !== refreshSeqRef.current
			) {
				return;
			}
			if (error instanceof CancelledError) return;
			setSession({ authenticated: false });
			setCsrfToken(null);
		} finally {
			if (
				!logoutInFlightRef.current &&
				epoch === epochRef.current &&
				seq === refreshSeqRef.current
			) {
				setLoading(false);
			}
		}
	}, [abortRefresh]);

	useEffect(() => {
		void refresh();
	}, [refresh]);

	useEffect(() => {
		setUnauthorizedHandler(() => {
			epochRef.current += 1;
			refreshSeqRef.current += 1;
			abortRefresh();
			setSession({ authenticated: false });
			setCsrfToken(null);
			qc.clear();
		});
		return () => setUnauthorizedHandler(null);
	}, [abortRefresh, qc]);

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
			const data = await apiFetch<{
				csrfToken?: string;
				username?: string;
				role?: UserRole;
				locale?: string;
			}>("/api/auth/login", {
				method: "POST",
				body: JSON.stringify({ username, password }),
			});
			const epoch = ++epochRef.current;
			logoutInFlightRef.current = false;
			refreshSeqRef.current += 1;
			abortRefresh();
			if (data?.csrfToken) {
				setCsrfToken(data.csrfToken);
			}
			const fromLogin = (): Session => {
				const session: Session = {
					authenticated: true,
					username: data.username ?? username,
				};
				if (data.role) session.role = data.role;
				if (data.locale) session.locale = data.locale;
				if (data.csrfToken) session.csrfToken = data.csrfToken;
				return session;
			};
			try {
				const status = await apiFetch<Session>("/api/auth/status");
				if (epoch !== epochRef.current) return;
				if (status.authenticated) {
					setSession(status);
					setCsrfToken(status.csrfToken ?? data.csrfToken ?? null);
				} else {
					setSession(fromLogin());
				}
			} catch {
				if (epoch !== epochRef.current) return;
				// Login already issued the session cookie; do not wipe CSRF if
				// status is briefly unavailable.
				setSession(fromLogin());
			} finally {
				if (epoch === epochRef.current) setLoading(false);
			}
			if (epoch !== epochRef.current) return;
			try {
				localStorage.setItem("veil_spa", "1");
			} catch {
				/* storage may be disabled */
			}
			broadcastRefresh();
		},
		[abortRefresh, broadcastRefresh],
	);

	const logout = useCallback(async () => {
		const epoch = ++epochRef.current;
		logoutInFlightRef.current = true;
		refreshSeqRef.current += 1;
		abortRefresh();
		try {
			await apiFetch("/api/auth/logout", { method: "POST" });
		} finally {
			if (epoch === epochRef.current) {
				setSession({ authenticated: false });
				setCsrfToken(null);
				qc.clear();
				try {
					localStorage.removeItem("veil_spa");
				} catch {
					/* storage may be disabled */
				}
				broadcastRefresh();
				logoutInFlightRef.current = false;
			}
		}
	}, [abortRefresh, broadcastRefresh, qc]);

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
