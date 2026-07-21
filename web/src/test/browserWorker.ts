// MSW browser worker for real-browser (Chromium) tests — blocker W8. The
// jsdom suite uses msw/node; a real browser needs the Service Worker variant.
// Handlers are shared with the node server via ./handlers (isomorphic) so
// both suites mock the same API.
import { setupWorker } from "msw/browser";
import { defaultHandlers } from "./handlers";

export const worker = setupWorker(...defaultHandlers);
