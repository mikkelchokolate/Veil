#!/usr/bin/env python3
"""i18n catalog checker for the Veil web SPA.

Checks:
 1. en/ru catalogs contain exactly the same keys.
 2. Every t("key") referenced in src exists in the en catalog (dynamic
    t(`ns.${var}`) templates are checked by prefix against catalog keys).

Hardcoded-string leak detection lives in scripts/i18n_leaks.mjs (AST-based,
@babel/parser): the regex approach used here previously matched only
single-line `>text<` and silently missed every multiline JSX text node.
pnpm i18n:check runs both.

Usage: python3 i18n_check.py  (exit 0 = clean)
"""
import re
import sys
from pathlib import Path

# Repository-relative: resolve src from the script's own location
# (web/scripts/i18n_check.py -> web/src), never from the caller's CWD or an
# absolute developer-machine path.
SRC = Path(__file__).resolve().parent.parent / "src"

def load_catalog(path: Path) -> set[str]:
    return set(re.findall(r'^\s*"([^"]+)":', path.read_text(), re.M))

def iter_tsx():
    for p in sorted(SRC.rglob("*.tsx")) + sorted(SRC.rglob("*.ts")):
        rel = p.relative_to(SRC)
        if any(seg in ("generated", "test", "locales") for seg in rel.parts):
            continue
        if p.name.endswith((".test.tsx", ".test.ts")):
            continue
        yield p

def main():
    en = load_catalog(SRC / "i18n/locales/en.ts")
    ru = load_catalog(SRC / "i18n/locales/ru.ts")
    problems = 0

    only_en, only_ru = en - ru, ru - en
    if only_en:
        problems += 1
        print(f"KEY PARITY: {len(only_en)} keys only in en: {sorted(only_en)[:10]}")
    if only_ru:
        problems += 1
        print(f"KEY PARITY: {len(only_ru)} keys only in ru: {sorted(only_ru)[:10]}")

    referenced = set()
    dynamic_prefixes = set()
    for p in iter_tsx():
        src = p.read_text()
        referenced |= set(re.findall(r'\bt\("([^"]+)"', src))
        dynamic_prefixes |= set(re.findall(r'\bt\(`([a-zA-Z]+)\.\$\{', src))
    missing = referenced - en
    if missing:
        problems += 1
        print(f"MISSING KEYS: {len(missing)} referenced but not in en catalog:")
        for k in sorted(missing):
            print(f"  {k}")
    # dynamic: at least one catalog key must exist per prefix
    for prefix in sorted(dynamic_prefixes):
        if not any(k.startswith(prefix + ".") for k in en):
            problems += 1
            print(f"DYNAMIC PREFIX with no catalog keys: {prefix}.*")

    print("\nCLEAN" if problems == 0 else f"\n{problems} PROBLEM GROUPS")
    return 0 if problems == 0 else 1

if __name__ == "__main__":
    sys.exit(main())
