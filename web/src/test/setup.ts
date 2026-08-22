import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";
import { server } from "./server";

Object.defineProperty(window, "scrollTo", {
	configurable: true,
	value: () => undefined,
});

// MSW: intercept fetch so tests run against a mock Veil API.
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
	server.resetHandlers();
	cleanup();
});
afterAll(() => server.close());
