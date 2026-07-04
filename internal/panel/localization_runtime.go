package panel

import (
	"encoding/json"
	"strings"
)

var translationCatalogs = map[string]map[string]string{
	LocaleEnglish: {
		"language.label":           "Language",
		"a11y.skip":                "Skip to content",
		"a11y.primaryNavigation":   "Primary navigation",
		"nav.dashboard":            "Dashboard",
		"nav.inbounds":             "Inbounds",
		"nav.routing":              "Routing Rules",
		"nav.warp":                 "WARP",
		"nav.diagnostics":          "System Tools",
		"nav.backups":              "Backups",
		"nav.users":                "Users",
		"nav.logout":               "Log Out",
		"auth.username":            "Username",
		"auth.password":            "Password",
		"auth.login":               "Log In",
		"auth.usernamePlaceholder": "Enter username",
		"auth.passwordPlaceholder": "Enter password",
		"auth.failed":              "Authentication failed",
		"setup.title":              "Set up Veil",
		"setup.summary":            "Create the first administrator before configuring proxy inbounds.",
		"setup.admin":              "Create administrator",
		"setup.localAccess":        "Local access",
		"setup.localNotice":        "Initial setup is available only on the loopback Panel. Public access stays disabled until authentication and TLS policy are satisfied.",
		"setup.backup":             "Backup and recovery",
		"setup.backupAck":          "I will preserve both the encrypted state and its state key in a protected backup.",
		"setup.complete":           "Complete setup",
		"setup.saving":             "Saving setup...",
		"setup.done":               "Setup complete. Opening sign in...",
		"setup.failed":             "Setup request failed: {error}",
		"users.title":              "Users",
		"users.addTitle":           "Add New User",
		"users.editTitle":          "Edit User: {username}",
		"users.create":             "Create User",
		"users.saveChanges":        "Save Changes",
		"backups.title":            "Backups",
		"apply.title":              "Apply Configuration",
		"validation.ready":         "Ready to apply",
		"dialog.close":             "Close dialog",
		"action.save":              "Save",
		"action.cancel":            "Cancel",
		"action.create":            "Create",
		"action.update":            "Update",
		"action.delete":            "Delete",
		"action.refresh":           "Refresh",
		"action.generate":          "Generate",
		"action.copy":              "Copy",
		"action.download":          "Download",
		"action.restore":           "Restore",
		"action.preview":           "Preview",
		"status.loading":           "Loading...",
		"status.ready":             "Ready",
		"status.online":            "ONLINE",
		"status.apiService":        "API Service:",
		"status.saved":             "Saved",
		"status.copied":            "Copied",
		"field.role":               "Role",
		"field.locale":             "Locale",
		"field.name":               "Name",
		"field.password":           "Password",
		"field.actions":            "Actions",
		"field.service":            "Service",
		"field.status":             "Status",
		"field.type":               "Type",
		"field.port":               "Port",
		"field.protocol":           "Protocol",
		"field.enabled":            "Enabled",
		"role.admin":               "Administrator",
		"role.viewer":              "Viewer",
		"role.viewerReadOnly":      "Viewer role is read-only. Admin role is required for this action.",
		"role.viewerReadOnlyShort": "Viewer role is read-only; admin required.",
		"confirm.deleteUser":       "Delete user {username}?",
		"confirm.revokeSession":    "Revoke this session?",
		"confirm.revokeCurrent":    "Revoke the current browser session? You will need to sign in again.",
		"confirm.restoreBackup":    "Restore backup {name}?",
		"confirm.apply":            "Apply the reviewed configuration?",
	},
	LocaleRussian: {
		"language.label":           "Язык",
		"a11y.skip":                "Перейти к содержимому",
		"a11y.primaryNavigation":   "Основная навигация",
		"nav.dashboard":            "Обзор",
		"nav.inbounds":             "Входящие подключения",
		"nav.routing":              "Маршрутизация",
		"nav.warp":                 "WARP",
		"nav.diagnostics":          "Системные инструменты",
		"nav.backups":              "Резервные копии",
		"nav.users":                "Пользователи",
		"nav.logout":               "Выйти",
		"auth.username":            "Имя пользователя",
		"auth.password":            "Пароль",
		"auth.login":               "Войти",
		"auth.usernamePlaceholder": "Введите имя пользователя",
		"auth.passwordPlaceholder": "Введите пароль",
		"auth.failed":              "Не удалось войти",
		"setup.title":              "Настройка Veil",
		"setup.summary":            "Создайте первого администратора перед настройкой входящих подключений.",
		"setup.admin":              "Создание администратора",
		"setup.localAccess":        "Локальный доступ",
		"setup.localNotice":        "Первичная настройка доступна только через локальную панель. Публичный доступ останется выключенным, пока не выполнены требования аутентификации и TLS.",
		"setup.backup":             "Резервное копирование и восстановление",
		"setup.backupAck":          "Я сохраню зашифрованное состояние и его ключ в защищённой резервной копии.",
		"setup.complete":           "Завершить настройку",
		"setup.saving":             "Сохраняем настройки...",
		"setup.done":               "Настройка завершена. Открываем вход...",
		"setup.failed":             "Ошибка запроса настройки: {error}",
		"users.title":              "Пользователи",
		"users.addTitle":           "Добавление пользователя",
		"users.editTitle":          "Редактирование пользователя: {username}",
		"users.create":             "Создать пользователя",
		"users.saveChanges":        "Сохранить изменения",
		"backups.title":            "Резервные копии",
		"apply.title":              "Применение конфигурации",
		"validation.ready":         "Можно применять",
		"dialog.close":             "Закрыть диалог",
		"action.save":              "Сохранить",
		"action.cancel":            "Отмена",
		"action.create":            "Создать",
		"action.update":            "Обновить",
		"action.delete":            "Удалить",
		"action.refresh":           "Обновить",
		"action.generate":          "Сгенерировать",
		"action.copy":              "Копировать",
		"action.download":          "Скачать",
		"action.restore":           "Восстановить",
		"action.preview":           "Предпросмотр",
		"status.loading":           "Загрузка...",
		"status.ready":             "Готово",
		"status.online":            "В СЕТИ",
		"status.apiService":        "Сервис API:",
		"status.saved":             "Сохранено",
		"status.copied":            "Скопировано",
		"field.role":               "Роль",
		"field.locale":             "Язык",
		"field.name":               "Название",
		"field.password":           "Пароль",
		"field.actions":            "Действия",
		"field.service":            "Сервис",
		"field.status":             "Состояние",
		"field.type":               "Тип",
		"field.port":               "Порт",
		"field.protocol":           "Протокол",
		"field.enabled":            "Включено",
		"role.admin":               "Администратор",
		"role.viewer":              "Наблюдатель",
		"role.viewerReadOnly":      "Роль наблюдателя доступна только для чтения. Для этого действия нужен администратор.",
		"role.viewerReadOnlyShort": "Только чтение; требуется администратор.",
		"confirm.deleteUser":       "Удалить пользователя {username}?",
		"confirm.revokeSession":    "Отозвать эту сессию?",
		"confirm.revokeCurrent":    "Отозвать текущую сессию браузера? Потребуется снова войти.",
		"confirm.restoreBackup":    "Восстановить резервную копию {name}?",
		"confirm.apply":            "Применить проверенную конфигурацию?",
	},
}

func SupportedLocales() []string {
	return []string{LocaleEnglish, LocaleRussian}
}

func TranslationCatalogs() map[string]map[string]string {
	catalogs := make(map[string]map[string]string, len(translationCatalogs))
	for locale, catalog := range translationCatalogs {
		copyCatalog := make(map[string]string, len(catalog))
		for key, value := range catalog {
			copyCatalog[key] = value
		}
		catalogs[locale] = copyCatalog
	}
	return catalogs
}

// jsonMarshal is swappable so tests can exercise the error branch of
// LocalizationRuntimeJS without importing encoding/json internals.
var jsonMarshal = json.Marshal

func LocalizationRuntimeJS() string {
	catalogs, err := jsonMarshal(translationCatalogs)
	if err != nil {
		return ""
	}
	return strings.Replace(localizationRuntimeTemplate, "__VEIL_TRANSLATION_CATALOGS__", string(catalogs), 1)
}

const localizationRuntimeTemplate = `(function () {
  const catalogs = __VEIL_TRANSLATION_CATALOGS__;
  const supported = ['en', 'ru'];
  const locale = supported.includes(window.veilLocale) ? window.veilLocale : 'en';
  const sourceKeys = Object.create(null);
  Object.entries(catalogs.en || {}).forEach(([key, value]) => {
    if (value) sourceKeys[value] = key;
  });

  window.veilLocale = locale;
  window.veilT = function (key, values) {
    const catalog = catalogs[window.veilLocale] || catalogs.en;
    let text = catalog[key] || catalogs.en[key] || key;
    Object.entries(values || {}).forEach(([name, value]) => {
      text = text.replaceAll('{' + name + '}', String(value));
    });
    return text;
  };

  window.veilValidationIssueText = function (issue) {
    if (!issue) return '';
    const prefix = 'validation.' + String(issue.code || '');
    const messageKey = prefix + '.message';
    const remediationKey = prefix + '.remediation';
    const translatedMessage = window.veilT(messageKey);
    const translatedRemediation = window.veilT(remediationKey);
    const message = translatedMessage === messageKey ? String(issue.message || '') : translatedMessage;
    const remediation = translatedRemediation === remediationKey ? String(issue.remediation || '') : translatedRemediation;
    return message + (remediation ? ' ' + remediation : '');
  };

  function translateValue(value) {
    const trimmed = String(value || '').trim();
    const key = sourceKeys[trimmed];
    if (!key) return value;
    return String(value).replace(trimmed, window.veilT(key));
  }

  function translateElement(element) {
    if (!(element instanceof Element)) return;
    ['placeholder', 'title', 'aria-label'].forEach((attribute) => {
      if (!element.hasAttribute(attribute)) return;
      const current = element.getAttribute(attribute);
      const translated = translateValue(current);
      if (translated !== current) element.setAttribute(attribute, translated);
    });
    if (element.matches('[data-veil-locale-select]')) {
      element.value = window.veilLocale;
      if (!element.dataset.veilLocaleBound) {
        element.dataset.veilLocaleBound = 'true';
        element.addEventListener('change', () => persistLocale(element.value));
      }
    }
  }

  function translateTree(root) {
    if (root.nodeType === Node.TEXT_NODE) {
      const translated = translateValue(root.nodeValue);
      if (translated !== root.nodeValue) root.nodeValue = translated;
      return;
    }
    if (!(root instanceof Element) && root !== document) return;
    if (root instanceof Element) translateElement(root);
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT);
    let node;
    while ((node = walker.nextNode())) {
      if (node.nodeType === Node.TEXT_NODE) {
        const translated = translateValue(node.nodeValue);
        if (translated !== node.nodeValue) node.nodeValue = translated;
      } else {
        translateElement(node);
      }
    }
  }

  async function persistLocale(nextLocale) {
    if (!supported.includes(nextLocale) || nextLocale === window.veilLocale) return;
    document.cookie = 'veil_locale=' + encodeURIComponent(nextLocale) + '; Path=/; Max-Age=31536000; SameSite=Lax';
    const csrf = window.veil_csrf_token || localStorage.getItem('veil_csrf_token') || '';
    if (csrf) {
      try {
        const response = await fetch('/api/auth/locale', {
          method: 'POST',
          credentials: 'same-origin',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrf
          },
          body: JSON.stringify({ locale: nextLocale })
        });
        if (!response.ok) throw new Error('locale update failed');
      } catch (error) {
        console.error(error);
      }
    }
    window.location.reload();
  }

  document.documentElement.lang = window.veilLocale;
  translateTree(document.documentElement);
  new MutationObserver((records) => {
    records.forEach((record) => record.addedNodes.forEach(translateTree));
  }).observe(document.documentElement, { childList: true, subtree: true });
})();`
