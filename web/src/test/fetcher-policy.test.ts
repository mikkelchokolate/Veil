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
		await vi.advanceTimersByTimeAsync(60_000);
		const TimeoutError = (fetcher as FetcherExports).TimeoutError;
		expect(TimeoutError).toBeTypeOf("function");
		const error = await outcome;
		expect(error).toBeInstanceOf(TimeoutError as new () => Error);
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
});
