import { setupServer } from "msw/node";
import { defaultHandlers, HttpResponse, http } from "./handlers";

// Node (jsdom) MSW server. Handlers live in ./handlers (isomorphic) so the
// real-browser worker can share them without importing msw/node.
export const server = setupServer(...defaultHandlers);

export { HttpResponse, http };
