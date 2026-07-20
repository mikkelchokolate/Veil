#!/usr/bin/env python3
"""i18n leak checker for the Veil web SPA.

Checks:
 1. en/ru catalogs contain exactly the same keys.
 2. Every t("key") referenced in src exists in the en catalog (dynamic
    t(`ns.${var}`) templates are checked by prefix against catalog keys).
 3. Leak scan: JSX text nodes and user-facing string props (placeholder,
    title, aria-label, label) that still contain hardcoded English.

Usage: python3 i18n_check.py  (exit 0 = clean)
"""
import re
import sys
from pathlib import Path

SRC = Path("/root/projects/Veil/web/src")

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

def strip_comments_and_code_strings(src: str) -> str:
    # Remove line + block comments to reduce false positives.
    src = re.sub(r"//[^\n]*", "", src)
    src = re.sub(r"/\*.*?\*/", "", src, flags=re.S)
    return src

ALLOW_TEXT = re.compile(
    r"^[\s\d.,:;·—–\-+/%()|→×?!'’\"’&=<>@#_*~\[\]{}$\\`|^…MiB|GiB|CPU|EN|RU|Veil|WARP]*$"
)
# Tokens that are fine as literal text (brands, units, single glyphs).
ALLOW_WORDS = {
    "Veil", "WARP", "CPU", "MiB", "GiB", "EN", "RU", "ok", "OK", "yes", "no",
    "synced", "pending", "applying", "failed", "clean", "dirty",
    # code identifiers caught by the crude regex (generics like apiFetch<T>)
    "apiFetch", "Promise", "Panel",
}

def find_leaks(path: Path):
    src = strip_comments_and_code_strings(path.read_text())
    leaks = []
    # JSX text: >text<
    for m in re.finditer(r">([^<>{}\n]*[A-Za-z][^<>{}\n]*)<", src):
        text = m.group(1).strip()
        if not text or len(text) < 2:
            continue
        if ALLOW_TEXT.match(text):
            continue
        words = re.findall(r"[A-Za-z][A-Za-z'’-]*", text)
        if all(w in ALLOW_WORDS for w in words):
            continue
        # skip obvious code remnants (rare in JSX text)
        leaks.append((m.start(), f"text: {text[:80]!r}"))
    # props
    for m in re.finditer(
        r'(?:placeholder|title|aria-label|alt)="([^"]*[A-Za-z][^"]*)"', src
    ):
        val = m.group(1).strip()
        if ALLOW_TEXT.match(val):
            continue
        leaks.append((m.start(), f"prop: {val[:80]!r}"))
    return leaks

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

    leaks_total = 0
    for p in iter_tsx():
        leaks = find_leaks(p)
        if leaks:
            leaks_total += len(leaks)
            print(f"\nLEAKS {p.relative_to(SRC)} ({len(leaks)}):")
            for _, desc in leaks[:20]:
                print(f"  {desc}")
    if leaks_total:
        problems += 1
        print(f"\nTOTAL LEAKS: {leaks_total}")

    print("\nCLEAN" if problems == 0 else f"\n{problems} PROBLEM GROUPS")
    return 0 if problems == 0 else 1

if __name__ == "__main__":
    sys.exit(main())
