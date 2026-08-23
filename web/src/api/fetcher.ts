let csrfToken: string | null = null;
let unauthorizedHandler: (() => void) | null = null;

export function setCsrfToken(token: string | null) {
	csrfToken = token;
}

/** Gate registers this so a 401 on any Panel API except login returns the operator to Sign in. */
export function setUnauthorizedHandler(handler: (() => void) | null) {
	unauthorizedHandler = handler;
}

function notifyUnauthorized(path: string, status: number) {
	if (status !== 401 || path === "/api/auth/login") return;
	unauthorizedHandler?.();
}

export interface ApiValidationIssue {
	code?: string;
	severity?: string;
	field?: string;
	inboundId?: string;
	message?: string;
	remediation?: string;
	source?: string;
}

export class ApiError extends Error {
	status: number;
	code: string | undefined;
	details: unknown;
	issues: ApiValidationIssue[] | undefined;
	body: unknown;

	constructor(
		status: number,
		message: string,
		code?: string,
		details?: unknown,
		issues?: ApiValidationIssue[],
		body?: unknown,
	) {
		super(message);
		this.name = "ApiError";
		this.status = status;
		this.code = code;
		this.details = details;
		this.issues = issues;
		this.body = body;
	}
}

export class TimeoutError extends Error {
	constructor(message = "API request timed out") {
		super(message);
		this.name = "TimeoutError";
	}
}

export class CancelledError extends Error {
	constructor(message = "API request was cancelled") {
		super(message);
		this.name = "CancelledError";
	}
}

function panelBasePath(): string {
	const element =
		typeof document !== "undefined" ? document.querySelector("base") : null;
	const href = element?.getAttribute("href") ?? "/";
	return href.endsWith("/") ? href.slice(0, -1) : href;
}

export function apiUrl(path: string): string {
	if (/^[a-z][a-z0-9+.-]*:/i.test(path) || path.startsWith("//")) {
		throw new Error("API path must be same-origin and relative");
	}
	if (!path.startsWith("/")) {
		throw new Error("API path must start with /");
	}
	return `${panelBasePath()}${path}`;
}

function isSafeMethod(method: string): boolean {
	return method === "GET" || method === "HEAD" || method === "OPTIONS";
}

function retryableStatus(status: number): boolean {
	return status === 502 || status === 503 || status === 504;
}

function abortError(error: unknown): boolean {
	return error instanceof DOMException
		? error.name === "AbortError"
		: error instanceof Error && error.name === "AbortError";
}

async function requestOnce(
	url: string,
	options: RequestInit,
	timeoutMs: number,
): Promise<Response> {
	const controller = new AbortController();
	let timedOut = false;
	let callerCancelled = options.signal?.aborted ?? false;
	const onCallerAbort = () => {
		callerCancelled = true;
		controller.abort();
	};
	options.signal?.addEventListener("abort", onCallerAbort, { once: true });
	const timeout = globalThis.setTimeout(() => {
		timedOut = true;
		controller.abort();
	}, timeoutMs);
	try {
		return await fetch(url, {
			...options,
			signal: controller.signal,
			redirect: "follow",
		});
	} catch (error) {
		if (abortError(error)) {
			if (callerCancelled) throw new CancelledError();
			if (timedOut) throw new TimeoutError();
		}
		throw error;
	} finally {
		globalThis.clearTimeout(timeout);
		options.signal?.removeEventListener("abort", onCallerAbort);
	}
}

function assertSameOriginRedirect(response: Response) {
	if (!response.redirected) return;
	const finalURL = new URL(response.url, window.location.href);
	if (finalURL.origin !== window.location.origin) {
		throw new ApiError(
			502,
			"Cross-origin API redirect rejected",
			"external_redirect",
		);
	}
}

export type ApiFetchOptions = RequestInit & {
	timeoutMs?: number;
	attempts?: number;
};

const defaultTimeoutMs = 15_000;
// Mutations wait for apply + service health (up to serviceHealthPollTimeout
// plus staging/restart). A 15s abort cancels the HTTP request and used to
// cancel apply itself, which left inbound creates looking like "Create failed"
// while the host kept applying.
const defaultMutationTimeoutMs = 60_000;

export function mutationErrorMessage(error: unknown, fallback: string): string {
	if (
		error instanceof ApiError ||
		error instanceof TimeoutError ||
		error instanceof CancelledError
	) {
		return error.message;
	}
	return fallback;
}

function requestInitFrom(options?: ApiFetchOptions): RequestInit {
	if (!options) return {};
	const init: RequestInit = { ...options };
	delete (init as ApiFetchOptions).timeoutMs;
	delete (init as ApiFetchOptions).attempts;
	return init;
}

export async function apiFetch<T>(
	path: string,
	options?: ApiFetchOptions,
): Promise<T> {
	const method = (options?.method ?? "GET").toUpperCase();
	const timeoutMs =
		options?.timeoutMs ??
		(isSafeMethod(method) ? defaultTimeoutMs : defaultMutationTimeoutMs);
	const attempts = options?.attempts ?? (isSafeMethod(method) ? 3 : 1);
	const headers = new Headers(options?.headers ?? {});
	if (!headers.has("Accept")) headers.set("Accept", "application/json");
	if (options?.body !== undefined && !headers.has("Content-Type")) {
		headers.set("Content-Type", "application/json");
	}
	if (csrfToken && !isSafeMethod(method))
		headers.set("X-CSRF-Token", csrfToken);
	const requestOptions: RequestInit = {
		credentials: "same-origin",
		...requestInitFrom(options),
		method,
		headers,
	};
	let response: Response | undefined;
	let lastError: unknown;
	for (let attempt = 0; attempt < attempts; attempt += 1) {
		try {
			response = await requestOnce(apiUrl(path), requestOptions, timeoutMs);
			if (!retryableStatus(response.status) || attempt === attempts - 1) break;
			await response.body?.cancel();
		} catch (error) {
			if (
				error instanceof TimeoutError ||
				error instanceof CancelledError ||
				attempt === attempts - 1
			) {
				throw error;
			}
			lastError = error;
		}
	}
	if (!response) throw lastError ?? new Error("API request failed");
	assertSameOriginRedirect(response);
	const text = await response.text();
	let body: unknown;
	if (text) {
		try {
			body = JSON.parse(text);
		} catch {
			body = text;
		}
	}
	if (!response.ok) {
		notifyUnauthorized(path, response.status);
		const maybe = body as
			| {
					error?: string | { message?: string; code?: string };
					message?: string;
					code?: string;
					details?: unknown;
					issues?: ApiValidationIssue[];
			  }
			| undefined;
		const errorValue =
			typeof maybe?.error === "string"
				? maybe.error
				: (maybe?.error?.message ?? maybe?.message ?? response.statusText);
		throw new ApiError(
			response.status,
			errorValue,
			typeof maybe?.error === "object" ? maybe.error.code : maybe?.code,
			maybe?.details,
			maybe?.issues,
			body,
		);
	}
	return body as T;
}
