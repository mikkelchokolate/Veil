import { defineConfig } from "orval";

// Generated from the verified backend contract. NEVER hand-edit the output
// (src/api/generated/**). The chain is: Go API -> docs/openapi.yaml -> Orval
// -> TS DTO + fetch mutators. A custom fetch mutator centralises credentials,
// CSRF, error normalisation, and the WebBasePath prefix.
export default defineConfig({
	veil: {
		input: {
			target: "../docs/openapi.yaml",
		},
		output: {
			mode: "tags-split",
			target: "./src/api/generated/endpoints.ts",
			schemas: "./src/api/generated/models",
			client: "fetch",
			clean: true,
			prettier: false,
			override: {
				mutator: {
					path: "./src/api/fetcher.ts",
					name: "apiFetch",
				},
			},
		},
	},
});
