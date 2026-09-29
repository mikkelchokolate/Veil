import {
	type PublicKeyCredentialRequestOptionsJSON,
	startAuthentication,
} from "@simplewebauthn/browser";
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
import type {
	UserRole,
	WebAuthnAssertionOptions,
} from "../api/generated/models";

export interface Session {
	authenticated: boolean;
	username?: string;
	role?: UserRole;
	locale?: string;
	csrfToken?: string;
	/** True when this cookie session already satisfied the account's second
	 * factor — factor-management flows can skip the password re-check (#1171). */
	secondFactor?: boolean;
}

/** Result of the password stage. When `secondFactorRequired` is set the
 * server minted a short-lived pending_2fa cookie instead of a session; call
 * verifySecondFactor to finish the login (#1172). */
export interface LoginResult {
	secondFactorRequired: boolean;
	secondFactorMethods?: string[];
	pendingExpiresAt?: string;
}

interface LoginResponseData {
	csrfToken?: string;
	username?: string;
	role?: UserRole;
	locale?: string;
	secondFactorRequired?: boolean;
	secondFactorMethods?: string[];
	secondFactor?: boolean;
	pendingExpiresAt?: string;
}

interface AuthContextValue {
	session: Session | null;
	loading: boolean;
	login: (username: string, password: string) => Promise<LoginResult>;
	verifySecondFactor: (args: {
		code?: string;
		recoveryCode?: string;
	}) => Promise<LoginResult>;
	/** Passkey completion of the pending_2fa stage (#1171): the server mints an
	 * assertion challenge, the browser runs navigator.credentials.get, and the
	 * result is posted back to finish. Throws the ApiError/WebAuthnError. */
	verifySecondFactorPasskey: () => Promise<LoginResult>;
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

	// finishLogin owns the post-credential bookkeeping shared by the
	// password-only and the pending_2fa verify paths (#1172).
	const finishLogin = useCallback(
		async (data: LoginResponseData, fallbackUsername: string) => {
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
					username: data.username ?? fallbackUsername,
				};
				if (data.role) session.role = data.role;
				if (data.locale) session.locale = data.locale;
				if (data.csrfToken) session.csrfToken = data.csrfToken;
				if (data.secondFactor) session.secondFactor = true;
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

	const login = useCallback(
		async (username: string, password: string): Promise<LoginResult> => {
			const data = await apiFetch<LoginResponseData>("/api/auth/login", {
				method: "POST",
				body: JSON.stringify({ username, password }),
			});
			// TOTP-enabled accounts stop at a pending_2fa challenge here — no
			// session exists yet, so finishLogin must NOT run (#1172).
			if (data?.secondFactorRequired) {
				const result: LoginResult = { secondFactorRequired: true };
				if (data.secondFactorMethods) {
					result.secondFactorMethods = data.secondFactorMethods;
				}
				if (data.pendingExpiresAt) {
					result.pendingExpiresAt = data.pendingExpiresAt;
				}
				return result;
			}
			await finishLogin(data, username);
			return { secondFactorRequired: false };
		},
		[finishLogin],
	);

	const verifySecondFactor = useCallback(
		async (args: {
			code?: string;
			recoveryCode?: string;
		}): Promise<LoginResult> => {
			const data = await apiFetch<LoginResponseData>(
				"/api/v1/auth/totp/verify",
				{
					method: "POST",
					body: JSON.stringify(args),
				},
			);
			await finishLogin(data, data?.username ?? "");
			return { secondFactorRequired: false };
		},
		[finishLogin],
	);

	// Passkey factor (#1171): begin returns the assertion challenge the
	// pending_2fa cookie scopes; finish mints the real session. The browser
	// ceremony errors (WebAuthnError — e.g. the user cancelled) are surfaced
	// to the caller untranslated so the view can phrase them.
	const verifySecondFactorPasskey =
		useCallback(async (): Promise<LoginResult> => {
			const options = await apiFetch<WebAuthnAssertionOptions>(
				"/api/v1/auth/webauthn/begin",
				{ method: "POST" },
			);
			const assertion = await startAuthentication({
				optionsJSON:
					options.publicKey as unknown as PublicKeyCredentialRequestOptionsJSON,
			});
			const data = await apiFetch<LoginResponseData>(
				"/api/v1/auth/webauthn/finish",
				{ method: "POST", body: JSON.stringify(assertion) },
			);
			await finishLogin(data, data?.username ?? "");
			return { secondFactorRequired: false };
		}, [finishLogin]);

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
		() => ({
			session,
			loading,
			login,
			verifySecondFactor,
			verifySecondFactorPasskey,
			logout,
			refresh,
		}),
		[
			session,
			loading,
			login,
			verifySecondFactor,
			verifySecondFactorPasskey,
			logout,
			refresh,
		],
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
