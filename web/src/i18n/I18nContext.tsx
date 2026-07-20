import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext, useState } from "react";
import { postApiAuthLocale } from "../api/generated/auth/auth";
import type { Locale } from "../api/generated/models";

// B-i18n: en + ru localization. Translations keyed by locale, then by key.
const translations: Record<Locale, Record<string, string>> = {
	en: {
		"nav.overview": "Overview",
		"nav.clients": "Clients",
		"nav.inbounds": "Inbounds",
		"nav.routing": "Routing",
		"nav.traffic": "Traffic",
		"nav.warp": "WARP",
		"nav.system": "System",
		"nav.backups": "Backups",
		"nav.users": "Users",
		"nav.settings": "Settings",
		"nav.apply": "Apply",
		"common.logout": "Logout",
		"common.loading": "Loading…",
		"common.save": "Save",
		"common.cancel": "Cancel",
		"common.delete": "Delete",
		"common.edit": "Edit",
		"common.create": "Create",
		"common.retry": "Retry",
		"common.error": "Error",
		"common.success": "Success",
	},
	ru: {
		"nav.overview": "Обзор",
		"nav.clients": "Клиенты",
		"nav.inbounds": "Входы",
		"nav.routing": "Маршрутизация",
		"nav.traffic": "Трафик",
		"nav.warp": "WARP",
		"nav.system": "Система",
		"nav.backups": "Бэкапы",
		"nav.users": "Пользователи",
		"nav.settings": "Настройки",
		"nav.apply": "Применить",
		"common.logout": "Выйти",
		"common.loading": "Загрузка…",
		"common.save": "Сохранить",
		"common.cancel": "Отмена",
		"common.delete": "Удалить",
		"common.edit": "Изменить",
		"common.create": "Создать",
		"common.retry": "Повторить",
		"common.error": "Ошибка",
		"common.success": "Успешно",
	},
};

interface I18nContextValue {
	locale: Locale;
	t: (key: string) => string;
	setLocale: (locale: Locale) => void;
}

const I18nContext = createContext<I18nContextValue | null>(null);

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

	function t(key: string): string {
		return translations[locale][key] ?? translations.en[key] ?? key;
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
