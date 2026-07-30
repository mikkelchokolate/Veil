import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { playwright } from "@vitest/browser-playwright";
import { defineConfig } from "vite";

// The SPA is served both at "/" and under a secret WebBasePath ("/<secret>/").
// A RELATIVE base ("./") makes the hashed asset URLs resolve correctly under
// either mount point, so direct navigation / refresh works without the
// server needing to know the secret at build time.
export default defineConfig({
	base: "./",
	plugins: [
		// S7: file-based router. Must run before react() per TanStack docs.
		tanstackRouter({ target: "react", autoCodeSplitting: true }),
		react(),
		tailwindcss(),
	],
	test: {
		// Two projects (blocker W8): the default jsdom suite, and a real-browser
		// Chromium suite (*.browser.test.*) that runs the app through an actual
		// browser engine with the MSW Service Worker. `pnpm test` stays jsdom-
		// only; `pnpm test:browser` runs the browser project in CI.
		projects: [
			{
				extends: true,
				test: {
					name: "jsdom",
					environment: "jsdom",
					setupFiles: ["./src/test/setup.ts"],
					globals: true,
					// The jsdom suite shares one MSW server and browser-global state from
					// setup.ts. Running files concurrently lets one file's afterEach cleanup
					// tear down another file's active render, so keep file execution serial.
					fileParallelism: false,
					css: false,
					include: ["src/**/*.test.{ts,tsx}"],
					exclude: ["src/**/*.browser.test.{ts,tsx}"],
				},
			},
			{
				extends: true,
				// pretty-format (via @testing-library) references Node's `global`;
				// shim it for the real-browser engine.
				define: { global: "globalThis" },
				test: {
					name: "browser",
					include: ["src/**/*.browser.test.{ts,tsx}"],
					browser: {
						enabled: true,
						provider: playwright(),
						headless: true,
						instances: [{ browser: "chromium" }],
					},
				},
			},
		],
	},
	server: {
		proxy: {
			// "/s/" (not "/s") — a bare "/s" prefix also matches /src/* and would
			// proxy every module request to the backend.
			"/api": "http://127.0.0.1:47359",
			"^/s/": "http://127.0.0.1:47359",
		},
	},
	build: {
		outDir: "dist",
		sourcemap: false,
		rolldownOptions: {
			output: {
				codeSplitting: {
					groups: [
						{
							name: "zrender",
							test: /node_modules[\\/](?:\.pnpm[\\/]zrender@[^\\/]+[\\/]node_modules[\\/])?zrender[\\/]/,
							priority: 40,
						},
						{
							name: "echarts",
							test: /node_modules[\\/](?:\.pnpm[\\/]echarts@[^\\/]+[\\/]node_modules[\\/])?echarts[\\/]/,
							priority: 30,
						},
						{
							name: "react",
							test: (id: string) =>
								id.includes("node_modules") &&
								(id.includes("react-dom") || id.includes("/react/")),
							priority: 20,
						},
						{
							name: "tanstack",
							test: /node_modules[\\/].*@tanstack/,
							priority: 20,
						},
					],
				},
			},
		},
	},
});
