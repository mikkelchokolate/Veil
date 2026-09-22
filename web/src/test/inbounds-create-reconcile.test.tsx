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
	// applyState overrides the GET /api/apply/state body; "fail" makes the
	// state endpoint error out. jobsFail does the same for the jobs list.
	applyState?: Record<string, unknown> | "fail";
	jobsFail?: boolean;
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
		if (url.endsWith("/api/apply/state")) {
			if (state.applyState === "fail") {
				return respond(errorBody("state down", "internal"), 500);
			}
			return respond(
				state.applyState ?? {
					desiredRevision: 2,
					appliedRevision: 1,
					state: "pending",
				},
			);
		}
		if (url.endsWith("/api/apply/jobs")) {
			if (state.jobsFail) {
				return respond(errorBody("jobs down", "internal"), 500);
			}
			const status =
				state.jobStatus === undefined ? "applying" : state.jobStatus;
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
			applyState: {
				desiredRevision: 2,
				appliedRevision: 2,
				state: "synced",
				lastSuccessfulJobId: "job-1",
			},
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
	it.each(["applying", "failed", "recovery_pending"])(
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

	// #689: a committed inbound with no succeeded apply job is saved-but-not-
	// live — reconcile must not paint success:true on an empty jobs list.
	it("reports success:false when the committed inbound has no apply job and the state is not synced", async () => {
		vi.useFakeTimers();
		const state: MockState = {
			posts: 0,
			firstPostHangs: true,
			secondPostHangs: true,
			committed: true,
			idempotencyKeys: [],
			jobStatus: null,
			applyState: {
				desiredRevision: 2,
				appliedRevision: 1,
				state: "pending",
			},
		};
		vi.stubGlobal("fetch", installFetchMock(state));
		const pending = settle(createInbound({ name: "edge" }, "edge"));
		await vi.advanceTimersByTimeAsync(120_000);
		const { value, error } = await pending;
		expect(error).toBeUndefined();
		expect(value?.success).toBe(false);
		expect(value?.reconciled).toBe(true);
		expect(value?.applyJob).toBeUndefined();
	});

	// #689 twin: a jobs-list fetch failure must not green the reconcile
	// either — without evidence of a succeeded job the outcome stays
	// "committed but unproven".
	it("reports success:false when the apply state fetch fails", async () => {
		vi.useFakeTimers();
		const state: MockState = {
			posts: 0,
			firstPostHangs: true,
			secondPostHangs: true,
			committed: true,
			idempotencyKeys: [],
			jobStatus: "succeeded",
			applyState: "fail",
		};
		vi.stubGlobal("fetch", installFetchMock(state));
		const pending = settle(createInbound({ name: "edge" }, "edge"));
		await vi.advanceTimersByTimeAsync(120_000);
		const { value, error } = await pending;
		expect(error).toBeUndefined();
		expect(value?.success).toBe(false);
		expect(value?.reconciled).toBe(true);
	});

	it("reports success:false when the jobs list fetch fails and the state is not synced", async () => {
		vi.useFakeTimers();
		const state: MockState = {
			posts: 0,
			firstPostHangs: true,
			secondPostHangs: true,
			committed: true,
			idempotencyKeys: [],
			jobsFail: true,
			applyState: {
				desiredRevision: 2,
				appliedRevision: 1,
				state: "pending",
			},
		};
		vi.stubGlobal("fetch", installFetchMock(state));
		const pending = settle(createInbound({ name: "edge" }, "edge"));
		await vi.advanceTimersByTimeAsync(120_000);
		const { value, error } = await pending;
		expect(error).toBeUndefined();
		expect(value?.success).toBe(false);
		expect(value?.reconciled).toBe(true);
		expect(value?.applyJob).toBeUndefined();
	});

	// #700: /api/apply/jobs is global newest-first — a concurrent mutation's
	// succeeded job at items[0] must not green this create while the current
	// desired revision is still unconverged. The attached job is the one the
	// state view correlates (here: the job owning the current revision).
	it("does not let a newer unrelated apply job decide the create's outcome", async () => {
		vi.useFakeTimers();
		const state: MockState = {
			posts: 0,
			firstPostHangs: true,
			secondPostHangs: true,
			committed: true,
			idempotencyKeys: [],
			jobStatus: "succeeded",
			applyState: {
				desiredRevision: 3,
				appliedRevision: 2,
				state: "pending",
			},
		};
		const fetchMock = installFetchMock(state);
		vi.stubGlobal(
			"fetch",
			vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
				const url = typeof input === "string" ? input : input.toString();
				if (url.endsWith("/api/apply/jobs")) {
					return respond({
						items: [
							// items[0]: a concurrent mutation's converged job — not
							// this create's evidence.
							{
								id: "job-foreign",
								desiredRevision: 2,
								baseRevision: 1,
								status: "succeeded",
								trigger: "mutation",
								createdAt: 2,
							},
							{
								id: "job-pending",
								desiredRevision: 3,
								baseRevision: 2,
								status: "applying",
								trigger: "mutation",
								createdAt: 1,
							},
						],
					});
				}
				return fetchMock(input, init);
			}),
		);
		const pending = settle(createInbound({ name: "edge" }, "edge"));
		await vi.advanceTimersByTimeAsync(120_000);
		const { value, error } = await pending;
		expect(error).toBeUndefined();
		expect(value?.success).toBe(false);
		expect(value?.reconciled).toBe(true);
		// The attached job correlates to the current desired revision, never
		// bare items[0].
		expect(value?.applyJob?.id).toBe("job-pending");
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
