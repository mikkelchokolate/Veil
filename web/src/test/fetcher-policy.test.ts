import { afterEach, describe, expect, it, vi } from "vitest";
import * as fetcher from "../api/fetcher";

type FetcherExports = typeof fetcher & Record<string, unknown>;

function jsonResponse(body: unknown, init: Partial<Response> = {}): Response {
	return {
		ok: true,
		status: 200,
		statusText: "OK",
		redirected: false,
		url: "http://localhost/api/test",
		text: async () => JSON.stringify(body),
		...init,
	} as Response;
}

function streamingJsonResponse(
	body: unknown,
	options: {
		delayMs?: number;
		signal?: AbortSignal | null | undefined;
		neverComplete?: boolean;
		fail?: Error;
	} = {},
): Response {
	const payload = new TextEncoder().encode(JSON.stringify(body));
	const stream = new ReadableStream<Uint8Array>({
		start(controller) {
			const fail = (error: unknown) => {
				try {
					controller.error(error);
				} catch {
					/* already closed or errored */
				}
			};
			const finish = () => {
				if (options.fail) {
					fail(options.fail);
					return;
				}
				controller.enqueue(payload);
				controller.close();
			};
			let timer: ReturnType<typeof setTimeout> | undefined;
			if (!options.neverComplete) {
				timer = setTimeout(finish, options.delayMs ?? 0);
			}
			options.signal?.addEventListener(
				"abort",
				() => {
					if (timer !== undefined) clearTimeout(timer);
					fail(new DOMException("aborted", "AbortError"));
				},
				{ once: true },
			);
		},
	});
	return new Response(stream, {
		status: 200,
		statusText: "OK",
		headers: { "Content-Type": "application/json" },
	});
}

describe("apiFetch request policy", () => {
	afterEach(() => {
		fetcher.setUnauthorizedHandler(null);
		vi.unstubAllGlobals();
		vi.useRealTimers();
		vi.restoreAllMocks();
	});

	it("rejects absolute and protocol-relative API inputs", () => {
		for (const value of [
			"https://attacker.example/api",
			"//attacker.example/api",
			"http://attacker.example/api",
		]) {
			expect(() => fetcher.apiUrl(value)).toThrow(
				/relative|absolute|same-origin/i,
			);
		}
	});

	it("honors a per-request timeout and does not forward it to fetch", async () => {
		vi.useFakeTimers();
		const fetchMock = vi.fn((_url: string, options?: RequestInit) => {
			expect(options && "timeoutMs" in options).toBe(false);
			expect(options && "attempts" in options).toBe(false);
			if (!options?.signal) {
				return Promise.reject(new Error("request had no timeout signal"));
			}
			return new Promise<Response>((_resolve, reject) => {
				options.signal?.addEventListener("abort", () => {
					reject(new DOMException("aborted", "AbortError"));
				});
			});
		});
		vi.stubGlobal("fetch", fetchMock);
		const pending = fetcher.apiFetch("/api/slow", { timeoutMs: 1_000 });
		const outcome = pending.then(
			() => undefined,
			(error: unknown) => error,
		);
		await vi.advanceTimersByTimeAsync(1_000);
		const TimeoutError = (fetcher as FetcherExports).TimeoutError;
		expect(await outcome).toBeInstanceOf(TimeoutError as new () => Error);
	});

	it("lets a GET opt out of the default retry budget", async () => {
		const fetchMock = vi.fn().mockRejectedValue(new TypeError("network"));
		vi.stubGlobal("fetch", fetchMock);
		await expect(
			fetcher.apiFetch("/api/once", { attempts: 1 }),
		).rejects.toThrow("network");
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});

	it("applies a bounded default timeout with a typed timeout error", async () => {
		vi.useFakeTimers();
		vi.stubGlobal(
			"fetch",
			vi.fn((_url: string, options?: RequestInit) => {
				if (!options?.signal) {
					return Promise.reject(new Error("request had no timeout signal"));
				}
				return new Promise<Response>((_resolve, reject) => {
					options.signal?.addEventListener("abort", () => {
						reject(new DOMException("aborted", "AbortError"));
					});
				});
			}),
		);
		const pending = fetcher.apiFetch("/api/slow");
		const outcome = pending.then(
			() => undefined,
			(error: unknown) => error,
		);
		await vi.advanceTimersByTimeAsync(15_000);
		const TimeoutError = (fetcher as FetcherExports).TimeoutError;
		expect(TimeoutError).toBeTypeOf("function");
		const error = await outcome;
		expect(error).toBeInstanceOf(TimeoutError as new () => Error);
	});

	it("gives mutations a longer default timeout than GET so apply health can finish", async () => {
		vi.useFakeTimers();
		vi.stubGlobal(
			"fetch",
			vi.fn((_url: string, options?: RequestInit) => {
				if (!options?.signal) {
					return Promise.reject(new Error("request had no timeout signal"));
				}
				return new Promise<Response>((_resolve, reject) => {
					options.signal?.addEventListener("abort", () => {
						reject(new DOMException("aborted", "AbortError"));
					});
				});
			}),
		);
		const pending = fetcher.apiFetch("/api/inbounds", {
			method: "POST",
			body: "{}",
		});
		const outcome = pending.then(
			() => "resolved",
			(error: unknown) => error,
		);
		await vi.advanceTimersByTimeAsync(15_000);
		let settled = false;
		void outcome.then(() => {
			settled = true;
		});
		await Promise.resolve();
		expect(settled).toBe(false);
		await vi.advanceTimersByTimeAsync(45_000);
		const TimeoutError = (fetcher as FetcherExports).TimeoutError;
		expect(await outcome).toBeInstanceOf(TimeoutError as new () => Error);
	});

	it("surfaces timeout and API errors instead of a generic fallback", () => {
		expect(
			fetcher.mutationErrorMessage(
				new fetcher.ApiError(422, "port in use"),
				"Create failed",
			),
		).toBe("port in use");
		expect(
			fetcher.mutationErrorMessage(new fetcher.TimeoutError(), "Create failed"),
		).toBe("API request timed out");
		expect(
			fetcher.mutationErrorMessage(new Error("boom"), "Create failed"),
		).toBe("Create failed");
	});

	it("retries safe GET failures but never retries a mutation", async () => {
		const safeFetch = vi
			.fn()
			.mockRejectedValueOnce(new TypeError("network"))
			.mockRejectedValueOnce(new TypeError("network"))
			.mockResolvedValueOnce(jsonResponse({ ok: true }));
		vi.stubGlobal("fetch", safeFetch);
		await expect(fetcher.apiFetch("/api/safe")).resolves.toEqual({ ok: true });
		expect(safeFetch).toHaveBeenCalledTimes(3);

		const mutationFetch = vi.fn().mockRejectedValue(new TypeError("network"));
		vi.stubGlobal("fetch", mutationFetch);
		await expect(
			fetcher.apiFetch("/api/mutate", { method: "POST", body: "{}" }),
		).rejects.toThrow("network");
		expect(mutationFetch).toHaveBeenCalledTimes(1);
	});

	it("does not send a POST when the caller signal is already aborted", async () => {
		const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ ok: true }));
		vi.stubGlobal("fetch", fetchMock);
		const controller = new AbortController();
		controller.abort();
		const outcome = await fetcher
			.apiFetch("/api/v1/clients", {
				method: "POST",
				body: JSON.stringify({ name: "canceled-test" }),
				signal: controller.signal,
				attempts: 1,
			})
			.then(
				() => undefined,
				(error: unknown) => error,
			);
		expect(outcome).toBeInstanceOf(fetcher.CancelledError);
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it("does not send a GET when the caller signal is already aborted", async () => {
		const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ ok: true }));
		vi.stubGlobal("fetch", fetchMock);
		const controller = new AbortController();
		controller.abort();
		await expect(
			fetcher.apiFetch("/api/v1/clients", { signal: controller.signal }),
		).rejects.toBeInstanceOf(fetcher.CancelledError);
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it("does not dispatch a retry after the caller cancels", async () => {
		const controller = new AbortController();
		const fetchMock = vi.fn().mockImplementationOnce(async () => {
			controller.abort();
			throw new TypeError("network");
		});
		vi.stubGlobal("fetch", fetchMock);
		await expect(
			fetcher.apiFetch("/api/safe", { signal: controller.signal }),
		).rejects.toBeInstanceOf(fetcher.CancelledError);
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});

	it("maps caller cancellation separately from timeout", async () => {
		const controller = new AbortController();
		vi.stubGlobal(
			"fetch",
			vi.fn(
				(_url: string, options?: RequestInit) =>
					new Promise<Response>((_resolve, reject) => {
						options?.signal?.addEventListener("abort", () =>
							reject(new DOMException("cancelled", "AbortError")),
						);
					}),
			),
		);
		const pending = fetcher.apiFetch("/api/cancel", {
			signal: controller.signal,
		});
		const outcome = pending.then(
			() => undefined,
			(error: unknown) => error,
		);
		controller.abort();
		const CancelledError = (fetcher as FetcherExports).CancelledError;
		expect(CancelledError).toBeTypeOf("function");
		const error = await outcome;
		expect(error).toBeInstanceOf(CancelledError as new () => Error);
	});

	it("times out a stalled response body after headers arrive", async () => {
		const fetchMock = vi.fn((_url: string, options?: RequestInit) =>
			Promise.resolve(
				streamingJsonResponse(
					{ ok: true },
					{ delayMs: 100, signal: options?.signal },
				),
			),
		);
		vi.stubGlobal("fetch", fetchMock);
		const started = Date.now();
		const outcome = await fetcher
			.apiFetch("/api/slow-body", { timeoutMs: 15, attempts: 1 })
			.then(
				() => "resolved" as const,
				(error: unknown) => error,
			);
		expect(outcome).toBeInstanceOf(fetcher.TimeoutError);
		expect(Date.now() - started).toBeLessThan(80);
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});

	it("cancels a body read when the caller aborts after headers", async () => {
		const controller = new AbortController();
		const fetchMock = vi.fn((_url: string, options?: RequestInit) =>
			Promise.resolve(
				streamingJsonResponse(
					{ ok: true },
					{ neverComplete: true, signal: options?.signal },
				),
			),
		);
		vi.stubGlobal("fetch", fetchMock);
		const pending = fetcher.apiFetch("/api/stream", {
			signal: controller.signal,
			attempts: 1,
		});
		await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
		await Promise.resolve();
		controller.abort();
		await expect(pending).rejects.toBeInstanceOf(fetcher.CancelledError);
	});

	it("still returns a fast body that finishes inside the timeout", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn((_url: string, options?: RequestInit) =>
				Promise.resolve(
					streamingJsonResponse(
						{ ok: true },
						{ delayMs: 5, signal: options?.signal },
					),
				),
			),
		);
		await expect(
			fetcher.apiFetch("/api/fast-body", { timeoutMs: 200, attempts: 1 }),
		).resolves.toEqual({ ok: true });
	});

	it("surfaces a body stream error after headers", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn((_url: string, options?: RequestInit) =>
				Promise.resolve(
					streamingJsonResponse(
						{ ok: true },
						{
							delayMs: 5,
							fail: new TypeError("body failed"),
							signal: options?.signal,
						},
					),
				),
			),
		);
		await expect(
			fetcher.apiFetch("/api/broken-body", { timeoutMs: 200, attempts: 1 }),
		).rejects.toThrow("body failed");
	});

	it("rejects a cross-origin redirect even when the final response is 200", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn().mockResolvedValue(
				jsonResponse(
					{ secret: "must not consume" },
					{
						redirected: true,
						url: "https://attacker.example/capture",
					},
				),
			),
		);
		await expect(fetcher.apiFetch("/api/redirect")).rejects.toThrow(
			/redirect|origin/i,
		);
	});

	it("carries validation issues from a 422 envelope on ApiError", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn().mockResolvedValue(
				jsonResponse(
					{
						error: {
							code: "configuration_failed_live_validation",
							message: "configuration failed live validation",
						},
						issues: [
							{
								code: "port_invalid",
								severity: "error",
								field: "port",
								message: "port must be between 1 and 65535",
								source: "livevalidation",
							},
						],
					},
					{ ok: false, status: 422, statusText: "Unprocessable Entity" },
				),
			),
		);
		const failure = await fetcher.apiFetch("/api/inbounds").then(
			() => undefined,
			(error: unknown) => error,
		);
		expect(failure).toBeInstanceOf(fetcher.ApiError);
		const apiError = failure as fetcher.ApiError;
		expect(apiError.status).toBe(422);
		expect(apiError.message).toBe("configuration failed live validation");
		expect(apiError.code).toBe("configuration_failed_live_validation");
		expect(apiError.issues).toHaveLength(1);
		expect(apiError.issues?.[0]?.field).toBe("port");
		expect(apiError.issues?.[0]?.message).toContain("between 1 and 65535");
	});

	it("preserves Retry-After on ApiError for lockout responses", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn().mockResolvedValue(
				new Response(JSON.stringify({ error: "too many login attempts" }), {
					status: 429,
					statusText: "Too Many Requests",
					headers: { "Retry-After": "42" },
				}),
			),
		);
		const failure = await fetcher
			.apiFetch("/api/auth/login", { method: "POST", body: "{}" })
			.then(
				() => undefined,
				(error: unknown) => error,
			);
		expect(failure).toBeInstanceOf(fetcher.ApiError);
		expect((failure as fetcher.ApiError).status).toBe(429);
		expect((failure as fetcher.ApiError).retryAfterSeconds).toBe(42);
	});

	it("leaves retryAfterSeconds undefined when no Retry-After is sent", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn().mockResolvedValue(
				new Response(JSON.stringify({ error: "unauthorized" }), {
					status: 401,
					statusText: "Unauthorized",
				}),
			),
		);
		const failure = await fetcher.apiFetch("/api/v1/clients").then(
			() => undefined,
			(error: unknown) => error,
		);
		expect(failure).toBeInstanceOf(fetcher.ApiError);
		expect((failure as fetcher.ApiError).retryAfterSeconds).toBeUndefined();
	});

	it("notifies the session handler on 401 except for login", async () => {
		const handler = vi.fn();
		fetcher.setUnauthorizedHandler(handler);
		vi.stubGlobal(
			"fetch",
			vi
				.fn()
				.mockResolvedValue(
					jsonResponse(
						{ error: { message: "unauthorized" } },
						{ ok: false, status: 401, statusText: "Unauthorized" },
					),
				),
		);
		await expect(fetcher.apiFetch("/api/v1/clients")).rejects.toBeInstanceOf(
			fetcher.ApiError,
		);
		expect(handler).toHaveBeenCalledTimes(1);
		await expect(
			fetcher.apiFetch("/api/auth/login", {
				method: "POST",
				body: JSON.stringify({ username: "a", password: "b" }),
			}),
		).rejects.toBeInstanceOf(fetcher.ApiError);
		expect(handler).toHaveBeenCalledTimes(1);
	});
});
