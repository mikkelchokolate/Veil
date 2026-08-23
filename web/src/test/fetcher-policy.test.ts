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
			fetcher.mutationErrorMessage(new fetcher.ApiError(422, "port in use"), "Create failed"),
		).toBe("port in use");
		expect(
			fetcher.mutationErrorMessage(new fetcher.TimeoutError(), "Create failed"),
		).toBe("API request timed out");
		expect(fetcher.mutationErrorMessage(new Error("boom"), "Create failed")).toBe(
			"Create failed",
		);
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
