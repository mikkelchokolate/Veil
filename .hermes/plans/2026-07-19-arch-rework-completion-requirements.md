# Текущая задача: завершить реализацию без упрощения требований

Продолжай работу в существующей ветке:

```text
feat/arch-rework
```

В ветке уже присутствуют backend readiness changes и первоначальная реализация React frontend.

Не начинай реализацию заново и не создавай второй frontend. Проведи аудит текущего состояния относительно:

```text
.hermes/plans/2026-07-18-arch-rework-backend-readiness-and-frontend.md
```

После аудита немедленно исправь все найденные gaps и доведи существующую реализацию до полного Definition of Done.

Наличие файла, страницы, endpoint, dependency или passing smoke test не означает, что соответствующий пункт выполнен. Проверяй фактическое поведение и полноту реализации.

# Не отклоняйся от требований

Не изменяй смысл, объём и обязательность требований задания.

Запрещено самостоятельно заменять обязательную функцию на:

* read-only страницу;
* placeholder;
* минимальный MVP;
* текстовую рекомендацию использовать CLI;
* статическую таблицу вместо полноценного управления;
* polling вместо требуемого realtime contract без технического обоснования;
* самописный компонент вместо явно требуемой библиотеки;
* ручной TypeScript DTO вместо исправления OpenAPI и повторной Orval generation;
* один smoke test вместо требуемых unit, browser и Playwright scenarios;
* пометку `future work`, `higher risk`, `later` или `out of scope`;
* частичный feature parity;
* интерфейс, который только отображает данные, когда backend уже поддерживает mutations.

В частности, фразы вида:

```text
Editing is higher-risk, so this page is read-only.
Use the CLI to change this safely.
This minimal implementation is sufficient.
The remaining functionality can be added later.
```

не являются допустимым выполнением задания.

Если операция рискованна, реализуй её безопасный UI:

* validation;
* apply plan;
* confirmation;
* RBAC;
* CSRF;
* desired/applied feedback;
* error handling;
* rollback visibility.

Риск операции не является основанием удалить её из UI.

# Существующая реализация является отправной точкой, а не готовым результатом

Сначала составь внутреннюю gap matrix:

```text
Requirement
Current implementation
Missing behavior
Required change
Test proving completion
```

Не останавливайся после составления matrix и не выдавай её вместо реализации.

Особенно проверь следующие области.

## Backend correctness

### Fully immutable revisions

Revision snapshot должен фиксировать не только management JSON state, но и всё normalized client state, влияющее на runtime:

* Clients;
* Bindings;
* enabled state;
* protocol settings;
* active credential ID/version;
* encrypted credential material либо безопасную ссылку на immutable credential version.

Apply или retry revision N не должен получать Clients, Bindings или Credentials из текущего mutable SQLite state.

Добавь regression test:

```text
1. Создать revision N с client credential A.
2. Создать revision N+1 с credential B.
3. Retry revision N.
4. Доказать, что runtime revision N использует A, а не B.
```

Fallback на current mutable state при отсутствии или ошибке snapshot запрещён для новых tracked revisions.

### Single Client mutation transaction

Удали архитектуру, при которой:

* Client service notifier запускает apply после каждой внутренней операции;
* handler после этого запускает дополнительный apply;
* rollback выполняется через последовательные публичные service methods.

Одна логическая Client mutation должна выполнять:

```text
BEGIN SQL TRANSACTION
save Client
save Bindings
save Credentials
create desired revision snapshot
COMMIT
run exactly one apply job
return exactly one mutation envelope
```

При ошибке до commit:

```text
ROLLBACK
```

Не выполняй apply для промежуточного Client без bindings или credentials.

Не считай compensating delete настоящей транзакцией.

### Legacy migration

Миграция legacy profiles не должна существовать только как ручная административная кнопка.

Реализуй upgrade flow:

* backup;
* migration marker/version;
* idempotent migration;
* verification;
* normalized runtime rendering;
* legacy profiles read-only после успешной миграции;
* безопасный recovery path.

### Traffic

Либо подключи реальные runtime TrafficProvider implementations для поддерживаемых протоколов, либо возвращай точное capability/unsupported состояние.

Не называй Stage 3 полностью реализованным, если collector запущен без единого реального provider.

## Frontend stack

Используй полный утверждённый стек:

```text
React
TypeScript strict
Vite

TanStack Router
TanStack Query
TanStack Table
React Hook Form
Zod
Orval

shadcn/ui
Base UI или Radix UI
Tailwind CSS
Lucide Icons
Apache ECharts

Vitest
Vitest Browser Mode
MSW
Playwright
Biome
pnpm
```

Не считать подключённую, но не используемую dependency выполнением требования.

### API types

Основные API requests и responses должны использовать Orval-generated types и functions.

Запрещено писать локальные интерфейсы:

```text
ClientDetail
BindingView
ApplyJob
TrafficSummary
Settings
Inbound
RoutingRule
```

если соответствующие схемы существуют или должны существовать в OpenAPI.

Если generated type отсутствует или неверен:

1. исправь Go/OpenAPI contract;
2. перегенерируй SDK и frontend client;
3. используй generated type.

### Router

Переведи router на file-based TanStack Router architecture.

Добавь Zod validation для search params.

Не используй:

```text
useSearch({ strict: false })
as Record<string, string | undefined>
```

для основных страниц.

Добавь route-level lazy loading и error boundaries.

### Clients

Используй TanStack Table, а не обычный вручную написанный `<table>`.

Реализуй полностью:

* aggregate summary;
* server-side pagination;
* typed URL filters;
* search debounce;
* protocol filter;
* inbound filter;
* group filter;
* quota state;
* expiry range;
* apply state;
* online state;
* server sorting;
* row selection;
* column visibility;
* responsive representation;
* detailed partial bulk result.

Client details должны поддерживать:

* edit;
* enable/disable;
* delete;
* attach inbound;
* detach inbound;
* enable/disable binding;
* credential generation;
* credential replacement;
* credential rotation;
* one-time secret dialog с очисткой state;
* subscriptions;
* traffic;
* audit.

### Apply

Добавь:

* global status indicator;
* jobs page;
* `/apply/jobs/$jobId`;
* operations;
* validation results;
* health checks;
* rollback details;
* retry;
* reconcile;
* apply plan;
* legacy apply history;
* persistent desired/applied mismatch warning.

### Traffic

Используй Apache ECharts.

Добавь:

* historical time series;
* supported time ranges;
* upload/download;
* client breakdown;
* inbound breakdown;
* protocol breakdown;
* telemetry provider state;
* stale and failed states;
* client binding breakdown.

Не показывай chart при отсутствии telemetry, но отсутствие provider не является основанием полностью не реализовывать chart component и supported-data flow.

### Administrative feature parity

Полностью перенеси функции существующей панели.

Не оставляй только read-only реализации для:

* Inbounds;
* Routing;
* WARP;
* Backups;
* Users;
* Settings.

Если backend поддерживает mutation, новый UI должен её поддерживать.

Обязательно реализуй:

* Inbound create/edit/delete/enable/disable;
* protocol-specific fields;
* validation and apply feedback;
* routing rule create/edit/delete;
* routing presets;
* WARP complete configuration;
* backup create/download/verify/restore/prune;
* restore job status;
* user create/edit/delete;
* role management;
* active session management;
* settings editing;
* key rotation;
* panel update where доступно в legacy UI.

Нельзя объявлять feature parity, пока функции заменены read-only таблицами.

### UI system

Установи и фактически используй:

* shadcn/ui;
* выбранные Base UI или Radix primitives;
* Tailwind;
* Lucide;
* design tokens.

Не оставляй основную панель на глобальных самописных классах:

```text
card
btn
data-table
input
badge
```

без полноценной component system.

### Localization

Существующий backend поддерживает locale.

Новый frontend должен поддерживать как минимум текущие языки старой панели, включая русский.

Не хардкодь весь интерфейс только на английском.

### Testing

Установи Playwright как dependency и добавь реальный configuration.

Реализуй все ранее перечисленные Playwright critical flows.

Четыре простых frontend tests или успешный `pnpm build` не являются выполнением testing Definition of Done.

# Доказательство выполнения

Для каждого обязательного требования в финальном отчёте покажи:

```text
Requirement
Implementation files
Tests
Executed command
Result
```

Не помечай пункт выполненным на основании:

* существования файла;
* комментария в коде;
* названия теста без его запуска;
* компиляции;
* ручного предположения;
* частичной read-only реализации.

Не утверждай, что весь запрос выполнен, пока каждый Definition of Done item либо:

1. реализован и подтверждён тестом;
2. честно указан как незавершённый.

Незавершённые обязательные пункты не разрешается переименовывать в optional, future work или out of scope.

# Границы задачи

Не выполняй несвязанный общий рефакторинг репозитория.

Не изменяй Caddy, ACME, installer, systemd, firewall, CLI или protocol runtime без непосредственной необходимости для:

* нового UI;
* normalized Clients;
* immutable apply;
* subscriptions;
* traffic;
* миграции;
* security;
* tests.

Найденные несвязанные проблемы запиши в:

```text
Out of scope findings
```

и не исправляй.

Ограничение scope запрещает лишнюю работу, но не разрешает сокращать обязательный объём текущего задания.
