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
			client: "react-query",
			clean: true,
			prettier: false,
			override: {
				mutator: {
					path: "./src/api/fetcher.ts",
					name: "apiFetch",
				},
				// S7: generate TanStack Query hooks from the OpenAPI contract.
				// Zod schemas and MSW mocks live in their OWN projects below
				// (veilZod / veilMsw) with separate output trees — this project
				// emits production DTOs and hooks only.
				query: {
					useQuery: true,
					useMutation: true,
				},
			},
		},
	},
	// S7: Zod schemas for runtime validation of request/response payloads.
	veilZod: {
		input: { target: "../docs/openapi.yaml" },
		output: {
			client: "zod",
			target: "./src/api/generated/zod/endpoints.zod.ts",
			mode: "tags-split",
			fileExtension: ".zod.ts",
			clean: true,
			prettier: false,
			override: { zod: { strict: { response: false } } },
		},
	},
	// S7: MSW handlers so browser/component tests mock the API at the network
	// edge instead of stubbing fetch. Generated as a mock override on the main
	// react-query project below (client "msw" is not a standalone output).
	// Blocker W5: the MSW project owns a SEPARATE output tree (generated/msw/**,
	// including its own schemas dir) and must never clean or rewrite the
	// production DTOs in generated/models — previously it clobbered them with
	// *.msw.ts rewrites.
	veilMsw: {
		input: { target: "../docs/openapi.yaml" },
		output: {
			client: "react-query",
			target: "./src/api/generated/msw/endpoints.ts",
			mode: "tags-split",
			fileExtension: ".msw.ts",
			schemas: "./src/api/generated/msw/models",
			clean: true,
			prettier: false,
			override: {
				mutator: { path: "./src/api/fetcher.ts", name: "apiFetch" },
				mock: { enabled: true, type: "msw", useExamples: true },
			},
		},
	},
});
