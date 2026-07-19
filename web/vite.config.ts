import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The SPA is served both at "/" and under a secret WebBasePath ("/<secret>/").
// A RELATIVE base ("./") makes the hashed asset URLs resolve correctly under
// either mount point, so direct navigation / refresh works without the
// server needing to know the secret at build time.
export default defineConfig({
	base: "./",
	plugins: [react()],
	test: {
		environment: "jsdom",
		setupFiles: ["./src/test/setup.ts"],
		globals: true,
		css: false,
	},
	server: {
		proxy: {
			"/api": "http://127.0.0.1:47359",
			"/s": "http://127.0.0.1:47359",
		},
	},
	build: {
		outDir: "dist",
		sourcemap: false,
		rollupOptions: {
			output: {
				manualChunks(id: string) {
					if (id.includes("node_modules")) {
						if (id.includes("react-dom") || id.includes("/react/")) return "react";
						if (id.includes("@tanstack")) return "tanstack";
						return "vendor";
					}
				},
			},
		},
	},
});
