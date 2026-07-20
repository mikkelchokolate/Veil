import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext, useState } from "react";
import { postApiAuthLocale } from "../api/generated/auth/auth";
import type { Locale } from "../api/generated/models";
import { en } from "./locales/en";
import { ru } from "./locales/ru";

// en + ru localization. Catalogs live in ./locales/<locale>.ts, keyed flat
// with dot namespaces (nav.*, common.*, <page>.*). t() falls back to en, then
// to the key itself, and interpolates {placeholders} from the vars argument.
const translations: Record<Locale, Record<string, string>> = { en, ru };

export type I18nVars = Record<string, string | number>;

interface I18nContextValue {
	locale: Locale;
	t: (key: string, vars?: I18nVars) => string;
	setLocale: (locale: Locale) => void;
}

const I18nContext = createContext<I18nContextValue | null>(null);

function interpolate(template: string, vars?: I18nVars): string {
	if (!vars) return template;
	return template.replace(/\{(\w+)\}/g, (m, name: string) =>
		name in vars ? String(vars[name]) : m,
	);
}

export function I18nProvider({
	children,
	initialLocale,
}: {
	children: ReactNode;
	initialLocale?: Locale;
}) {
	const [locale, setLocaleState] = useState<Locale>(initialLocale ?? "en");
	const qc = useQueryClient();

	const setLocaleMutation = useMutation({
		mutationFn: (loc: Locale) => postApiAuthLocale({ locale: loc }),
		onSuccess: () => {
			void qc.invalidateQueries({ queryKey: ["auth"] });
		},
	});

	function setLocale(loc: Locale) {
		setLocaleState(loc);
		setLocaleMutation.mutate(loc);
	}

	function t(key: string, vars?: I18nVars): string {
		const raw = translations[locale][key] ?? translations.en[key] ?? key;
		return interpolate(raw, vars);
	}

	return (
		<I18nContext.Provider value={{ locale, t, setLocale }}>
			{children}
		</I18nContext.Provider>
	);
}

export function useI18n(): I18nContextValue {
	const ctx = useContext(I18nContext);
	if (!ctx) throw new Error("useI18n must be used within I18nProvider");
	return ctx;
}
