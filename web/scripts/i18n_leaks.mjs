#!/usr/bin/env node
import { readdirSync, readFileSync, statSync } from "node:fs";
/** AST-based i18n leak scanner for the Veil web SPA (issue 4).
 *
 * The old regex leak scan matched `>text<` on a single line, so any JSX text
 * that spanned a newline escaped it entirely. This scanner parses every .tsx
 * file with @babel/parser (jsx + typescript plugins) and walks the real AST:
 *
 *   - JSXText nodes (any shape, any line span) with user-facing English.
 *   - User-facing JSX attributes with string-literal values:
 *     placeholder, title, aria-label, alt.
 *
 * Allow-listed: punctuation/digit-only text, brand/unit tokens (Veil, WARP,
 * MiB...), and the single-word status vocabulary the panel renders as-is.
 *
 * Usage: node scripts/i18n_leaks.mjs  (exit 0 = clean)
 */
import { createRequire } from "node:module";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const parser = createRequire(import.meta.url)("@babel/parser");

const SRC = fileURLToPath(new URL("../src", import.meta.url));

const SKIP_DIRS = new Set(["generated", "test", "locales", "node_modules"]);
const USER_FACING_ATTRS = new Set([
	"placeholder",
	"title",
	"aria-label",
	"alt",
]);
// Tokens that are fine as literal text (brands, units, status glyphs).
const ALLOW_WORDS = new Set([
	"Veil",
	"WARP",
	"CPU",
	"MiB",
	"GiB",
	"EN",
	"RU",
	"ok",
	"OK",
	"yes",
	"no",
	"synced",
	"pending",
	"applying",
	"failed",
	"clean",
	"dirty",
	// API enum tokens rendered verbatim (severity fallback, locale codes).
	"info",
	"warning",
	"en",
	"ru",
]);

function* walkFiles(dir) {
	for (const entry of readdirSync(dir)) {
		const full = join(dir, entry);
		const st = statSync(full);
		if (st.isDirectory()) {
			if (!SKIP_DIRS.has(entry)) yield* walkFiles(full);
			continue;
		}
		if (entry.endsWith(".tsx") && !entry.endsWith(".test.tsx")) yield full;
	}
}

/** A text chunk is allowed when it carries no user-facing English: either it
 * has no Latin letters at all (punctuation/digits/symbols), or every Latin
 * word in it is explicitly allow-listed. */
function isAllowed(text) {
	const words = text.match(/[A-Za-z][A-Za-z'’-]*/g);
	if (!words) return true;
	return words.every((w) => ALLOW_WORDS.has(w));
}

/** Depth-first walk over a Babel AST with an ancestry stack, so string
 * literals in conditional/logical branches (e.g. `{ok ? "configured" : "not
 * set"}`) can be told apart from prop VALUES (`variant={ok ? "a" : "b"}`):
 * only branches rendered as element children are user-facing text. */
function walk(node, visit, ancestors = []) {
	if (!node || typeof node.type !== "string") return;
	visit(node, ancestors);
	const next = [...ancestors, node];
	for (const key of Object.keys(node)) {
		if (
			key === "loc" ||
			key === "leadingComments" ||
			key === "trailingComments"
		)
			continue;
		const child = node[key];
		if (Array.isArray(child)) {
			for (const c of child) walk(c, visit, next);
		} else if (child && typeof child.type === "string") {
			walk(child, visit, next);
		}
	}
}

function checkFile(path, problems) {
	const code = readFileSync(path, "utf8");
	const ast = parser.parse(code, {
		sourceType: "module",
		plugins: ["jsx", "typescript"],
		errorRecovery: false,
	});
	const rel = relative(SRC, path);
	const report = (node, kind, raw) => {
		const text = String(raw).replace(/\s+/g, " ").trim();
		if (text.length < 2 || isAllowed(text)) return;
		const line = node.loc?.start.line ?? "?";
		problems.push(
			`${rel}:${line} ${kind}: ${JSON.stringify(text.slice(0, 80))}`,
		);
	};

	walk(ast.program, (node, ancestors) => {
		if (node.type === "JSXText") {
			report(node, "jsx-text", node.value);
		} else if (node.type === "JSXAttribute") {
			const name =
				node.name?.type === "JSXIdentifier" ? node.name.name : undefined;
			if (
				name &&
				USER_FACING_ATTRS.has(name) &&
				node.value?.type === "StringLiteral"
			) {
				report(node, `attr ${name}`, node.value.value);
			}
		} else if (node.type === "StringLiteral" && ancestors.length >= 2) {
			// {cond ? "Shown text" : "Other text"} / {cond && "Shown text"} —
			// string literals rendered as element children through a JSX
			// expression. The container must sit on a JSXElement (rendered
			// child); a container under a JSXAttribute is a prop VALUE (variant,
			// className, key), not user-facing text.
			const parent = ancestors[ancestors.length - 1];
			const branchOfConditional =
				parent.type === "ConditionalExpression" &&
				(parent.consequent === node || parent.alternate === node);
			const branchOfLogical =
				parent.type === "LogicalExpression" && parent.right === node;
			if (!branchOfConditional && !branchOfLogical) return;
			const container = ancestors.findLast(
				(a) => a.type === "JSXExpressionContainer",
			);
			if (!container) return;
			const holder = ancestors[ancestors.indexOf(container) - 1];
			if (!holder || holder.type !== "JSXElement") return;
			report(node, "jsx-expr string", node.value);
		}
	});
}

const problems = [];
for (const file of walkFiles(SRC)) {
	checkFile(file, problems);
}

if (problems.length > 0) {
	console.error(`i18n leaks (AST scan): ${problems.length}`);
	for (const p of problems) console.error(`  ${p}`);
	process.exit(1);
}
console.log("CLEAN");
