// Central fetch mutator used by every Orval-generated client call.
//
// Responsibilities:
//  - Prefix the panel WebBasePath so API calls resolve under "/<secret>" too.
//  - Same-origin credentials (the HTTP-only veil_session cookie).
//  - Attach X-CSRF-Token for cookie-authenticated mutations only.
//  - Normalise the heterogeneous backend error shapes into a thrown ApiError.
//
// NOTHING here is persisted to storage — no admin tokens, no credentials.

let csrfToken: string | null = null;

/** Called once after /api/auth/status succeeds so mutations can carry CSRF. */
export function setCsrfToken(token: string | null): void {
	csrfToken = token;
}

export class ApiError extends Error {
	readonly status: number;
	readonly code: string | undefined;
	readonly details: unknown;

	constructor(
		status: number,
		message: string,
		code?: string,
		details?: unknown,
	) {
		super(message);
		this.name = "ApiError";
		this.status = status;
		this.code = code;
		this.details = details;
	}
}

/** Base path the SPA is mounted under ("" at root, "/<secret>" otherwise). */
function panelBasePath(): string {
	const el =
		typeof document !== "undefined" ? document.querySelector("base") : null;
	const href = el?.getAttribute("href") ?? "/";
	// href is like "/" or "/secret/". Strip trailing slash; "" means root.
	return href.endsWith("/") ? href.slice(0, -1) : href;
}

export function apiUrl(path: string): string {
	return `${panelBasePath()}${path}`;
}

function isMutating(method: string): boolean {
	const m = method.toUpperCase();
	return m !== "GET" && m !== "HEAD" && m !== "OPTIONS";
}

function extractMessage(
	body: unknown,
	fallback: string,
): { message: string; code?: string } {
	if (body && typeof body === "object") {
		const o = body as Record<string, unknown>;
		const nested =
			o.error && typeof o.error === "object"
				? (o.error as Record<string, unknown>)
				: undefined;
		const message =
			(typeof nested?.message === "string" && nested.message) ||
			(typeof o.message === "string" && o.message) ||
			(typeof o.error === "string" && o.error) ||
			fallback;
		const code =
			typeof nested?.code === "string"
				? nested.code
				: typeof o.code === "string"
					? o.code
					: undefined;
		return { message, ...(code ? { code } : {}) };
	}
	return { message: fallback };
}

export async function apiFetch<T>(
	url: string,
	options: RequestInit = {},
): Promise<T> {
	const method = (options.method ?? "GET").toUpperCase();
	const headers = new Headers(options.headers);
	if (options.body != null && !headers.has("Content-Type")) {
		headers.set("Content-Type", "application/json");
	}
	// CSRF only for cookie-authenticated mutations.
	if (isMutating(method) && csrfToken) {
		headers.set("X-CSRF-Token", csrfToken);
	}

	const res = await fetch(apiUrl(url), {
		...options,
		method,
		headers,
		credentials: "same-origin",
	});

	if (res.status === 204) {
		return undefined as T;
	}

	const text = await res.text();
	let body: unknown;
	try {
		body = text ? JSON.parse(text) : undefined;
	} catch {
		body = text;
	}

	if (!res.ok) {
		const { message, code } = extractMessage(
			body,
			res.statusText || `HTTP ${res.status}`,
		);
		throw new ApiError(res.status, message, code, body);
	}

	return body as T;
}
