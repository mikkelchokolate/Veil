import { createInbound } from "../pages/inboundsCreateRecovery";

// Regression for #312: a committed create keeps applying server-side after
// the browser's mutation deadline aborts the request. The create helper
// retries under the same Idempotency-Key and, when no response can be
// obtained, reconciles the committed inbound and its apply job instead of
// leaving a retryable Create form that hits duplicate-name.

function respond(body: unknown, status = 200): Promise<Response> {
	return Promise.resolve(
		new Response(JSON.stringify(body), {
			status,
			headers: { "Content-Type": "application/json" },
		}),
	);
}

function errorBody(message: string, code = "conflict"): unknown {
	return { error: { code, message } };
}

function hangingRequest(init?: RequestInit): Promise<Response> {
	return new Promise<Response>((_resolve, reject) => {
		init?.signal?.addEventListener("abort", () => {
			reject(new DOMException("aborted", "AbortError"));
		});
	});
}

interface MockState {
	posts: number;
	firstPostHangs: boolean;
	secondPostHangs: boolean;
	committed: boolean;
	idempotencyKeys: Array<string | null>;
	// jobStatus is the latest apply job status returned by /api/apply/jobs;
	// null means the jobs list comes back empty.
	jobStatus?: string | null;
}

function installFetchMock(state: MockState) {
	return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
		const url = typeof input === "string" ? input : input.toString();
		const method = (init?.method ?? "GET").toUpperCase();
		if (url.endsWith("/api/inbounds") && method === "POST") {
			state.posts += 1;
			state.idempotencyKeys.push(
				new Headers(init?.headers).get("Idempotency-Key"),
			);
			if (state.posts === 1 && state.firstPostHangs) {
				return hangingRequest(init);
			}
			if (state.posts === 2 && state.secondPostHangs) {
				return hangingRequest(init);
			}
			return respond({ name: "edge", success: true }, 201);
		}
		if (url.endsWith("/api/inbounds")) {
			return respond(
				state.committed
					? [
							{
								name: "edge",
								protocol: "hysteria2",
								transport: "udp",
								port: 443,
								enabled: true,
							},
						]
					: [],
			);
		}
		if (url.endsWith("/api/apply/jobs")) {
			const status = state.jobStatus === undefined ? "running" : state.jobStatus;
			return respond({
				items:
					status === null
						? []
						: [
								{
									id: "job-1",
									desiredRevision: 2,
									baseRevision: 1,
									status,
									trigger: "mutation",
									createdAt: 1,
								},
							],
			});
		}
		return respond({});
	});
}

async function settle<T>(
	promise: Promise<T>,
): Promise<{ value?: T; error?: unknown }> {
	return promise.then(
		(value) => ({ value }),
		(error) => ({ error }),
	);
}

describe("createInbound recovery", () => {
	afterEach(() => {
		vi.unstubAllGlobals();
		vi.useRealTimers();
	});

	it("returns the create response when the first request succeeds", async () => {
		const state: MockState = {
			posts: 0,
			firstPostHangs: false,
			secondPostHangs: false,
			committed: false,
			idempotencyKeys: [],
		};
		vi.stubGlobal("fetch", installFetchMock(state));
		const outcome = await createInbound({ name: "edge" }, "edge");
		expect(outcome.success).toBe(true);
		expect(state.posts).toBe(1);
		expect(state.idempotencyKeys[0]).toBeTruthy();
	});

	it("replays the timed-out create under the same Idempotency-Key", async () => {
		vi.useFakeTimers();
		const state: MockState = {
			posts: 0,
			firstPostHangs: true,
			secondPostHangs: false,
			committed: true,
			idempotencyKeys: [],
		};
		vi.stubGlobal("fetch", installFetchMock(state));
		const pending = settle(createInbound({ name: "edge" }, "edge"));
		await vi.advanceTimersByTimeAsync(60_000);
		const { value, error } = await pending;
		expect(error).toBeUndefined();
		expect(value?.success).toBe(true);
		expect(state.posts).toBe(2);
		expect(state.idempotencyKeys).toHaveLength(2);
		expect(state.idempotencyKeys[0]).toBeTruthy();
		expect(state.idempotencyKeys[0]).toBe(state.idempotencyKeys[1]);
	});

	it("reconciles the committed inbound and its apply job when no response arrives", async () => {
		vi.useFakeTimers();
		const state: MockState = {
			posts: 0,
			firstPostHangs: true,
			secondPostHangs: true,
			committed: true,
			idempotencyKeys: [],
			jobStatus: "succeeded",
		};
		vi.stubGlobal("fetch", installFetchMock(state));
		const pending = settle(createInbound({ name: "edge" }, "edge"));
		await vi.advanceTimersByTimeAsync(120_000);
		const { value, error } = await pending;
		expect(error).toBeUndefined();
		expect(value?.success).toBe(true);
		expect(value?.reconciled).toBe(true);
		expect(value?.applyJob?.id).toBe("job-1");
		expect(state.posts).toBe(2);
	});

	// Regression for #380: reconcile must derive success from the attached job
	// like the server does — anything other than succeeded means saved-but-not-
	// live, never a green "saved".
	it.each(["running", "failed", "recovery_pending"])(
		"reports success:false when the reconciled job is %s",
		async (jobStatus) => {
			vi.useFakeTimers();
			const state: MockState = {
				posts: 0,
				firstPostHangs: true,
				secondPostHangs: true,
				committed: true,
				idempotencyKeys: [],
				jobStatus,
			};
			vi.stubGlobal("fetch", installFetchMock(state));
			const pending = settle(createInbound({ name: "edge" }, "edge"));
			await vi.advanceTimersByTimeAsync(120_000);
			const { value, error } = await pending;
			expect(error).toBeUndefined();
			expect(value?.success).toBe(false);
			expect(value?.reconciled).toBe(true);
			expect(value?.applyJob?.status).toBe(jobStatus);
		},
	);

	it("reports success:true when the committed inbound has no apply job", async () => {
		vi.useFakeTimers();
		const state: MockState = {
			posts: 0,
			firstPostHangs: true,
			secondPostHangs: true,
			committed: true,
			idempotencyKeys: [],
			jobStatus: null,
		};
		vi.stubGlobal("fetch", installFetchMock(state));
		const pending = settle(createInbound({ name: "edge" }, "edge"));
		await vi.advanceTimersByTimeAsync(120_000);
		const { value, error } = await pending;
		expect(error).toBeUndefined();
		expect(value?.success).toBe(true);
		expect(value?.reconciled).toBe(true);
		expect(value?.applyJob).toBeUndefined();
	});

	it("propagates a replayed rejection instead of falsely claiming a commit", async () => {
		vi.useFakeTimers();
		const state: MockState = {
			posts: 0,
			firstPostHangs: true,
			secondPostHangs: false,
			committed: false,
			idempotencyKeys: [],
		};
		const fetchMock = installFetchMock(state);
		vi.stubGlobal(
			"fetch",
			vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
				const url = typeof input === "string" ? input : input.toString();
				const method = (init?.method ?? "GET").toUpperCase();
				if (
					url.endsWith("/api/inbounds") &&
					method === "POST" &&
					state.posts === 1
				) {
					// The idempotent replay of a rejected create.
					state.posts += 1;
					return respond(errorBody("invalid port", "invalid_request"), 400);
				}
				return fetchMock(input, init);
			}),
		);
		const pending = settle(createInbound({ name: "edge" }, "edge"));
		await vi.advanceTimersByTimeAsync(60_000);
		const { value, error } = await pending;
		expect(value).toBeUndefined();
		expect(error).toBeInstanceOf(Error);
		expect((error as { status?: number }).status).toBe(400);
	});

	it("rethrows the timeout when nothing was committed", async () => {
		vi.useFakeTimers();
		const state: MockState = {
			posts: 0,
			firstPostHangs: true,
			secondPostHangs: true,
			committed: false,
			idempotencyKeys: [],
		};
		vi.stubGlobal("fetch", installFetchMock(state));
		const pending = settle(createInbound({ name: "edge" }, "edge"));
		await vi.advanceTimersByTimeAsync(120_000);
		const { value, error } = await pending;
		expect(value).toBeUndefined();
		expect(error).toBeInstanceOf(Error);
		expect((error as Error).name).toBe("TimeoutError");
	});
});
