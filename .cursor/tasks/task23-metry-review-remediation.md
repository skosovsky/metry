# Task 23. Metry: исправить дефекты telemetry и привести публичные контракты в порядок

Статус: **выполнено**. T01–T14 приняты: независимая полнота 100%, корректность без неустранённых замечаний; обязательные проверки прошли. Текущий реестр: [task23-progress.md](task23-progress.md).

Ниже сохранён исходный review baseline, а не описание текущих дефектов. Дата независимого ревью: 2026-10-06. Проверенный commit: `9b6bd144b25f0a737ca185aebb7f0552493ad398` (`feat: agent telemetry`). Разрешён clear break, включая удаление API и реорганизацию пакетов. Исходники в ходе ревью не исправлялись.

## Цель и вывод ревью

Сохранить metry независимой библиотекой наблюдаемости, устранить подтверждённую потерю/искажение telemetry, сделать privacy и lifecycle единообразными, убрать необоснованные обходные API. Это задача на исправление по результатам ревью, а не новая реализация всей библиотеки.

Архитектурная основа хорошая: provider-local SDK dependencies, явное владение ресурсами, необязательный gRPC-модуль, presence-aware measurements, bounded payloads, отдельные logical/physical identities. В просмотренных путях не обнаружено ошибочного суммирования inclusive totals/components, повторного учёта промежуточного streaming usage или переноса billing/eval execution в metry. Однако успешные штатные проверки не покрывают несколько воспроизведённых ошибок публичного API.

Работали шесть независимых субагентов: архитектура/границы, usage/evaluations, privacy, concurrency/lifecycle, integrations и документация/naming. Основные runtime-дефекты дополнительно воспроизведены ведущим ревьюером. Ни один пункт ниже не является доказательством отсутствия других ошибок.

## Проверки и ограничения

| Проверка | Результат |
|---|---|
| `make test` | PASS: root, middleware/grpc, integration/otlp; `go test -v -race ./...` для каждого модуля |
| `make lint` | PASS: 0 issues во всех трёх модулях |
| `make test-examples` | PASS: 9 примеров, включая agent_workflow |
| Целевые usage/stream/evaluation tests | PASS |
| Публичные repro: span status, snapshot, scope privacy, enrichment, fallback | Ошибочное поведение подтверждено |
| Release staging | Ошибка воспроизведена агентом в отдельном временном Git-репозитории; реальная публикация не запускалась |

Первый запуск workspace-проверок упёрся в права записи Go module cache. После разрешённого доступа зависимости загрузились, повторные полные проверки прошли. Это ограничение окружения, а не дефект библиотеки. Fuzz/bench/full release и проверка реальных внешних backends не выполнялись. Проверялась локальная реализация на указанном commit; соответствие опубликованного release-артефакта этому checkout отдельно не доказано.

[Логи и отчёты субагентов](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/ai-libs/reviews/metry-2026-10-06/evidence>) и [исходники воспроизведений](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/ai-libs/reviews/metry-2026-10-06/repro>) сохранены отдельно. Repro запускаются по одному через `GOWORK=off GOCACHE=/tmp/metry-review-gocache go run /absolute/path/to/repro.go` из корня metry; это несколько самостоятельных `package main`, не один Go-пакет. Для реализации достаточно контрактов и AAA-сценариев в этой задаче.

## Подтверждённые замечания

Приоритет P1 означает исправить прежде следующего релиза; P2 — исправить в рамках этой задачи; P3 — документация и согласование API. Это приоритет работ, а не оценка удалённой эксплуатируемости.

### MTR-R01 · P1 — завершение span переписывает Error в Ok

**Код:** [internal/traceutil/span.go:59](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/internal/traceutil/span.go:59>), [provider_span.go:46](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/provider_span.go:46>).

`SpanOKIfUnset` пытается читать статус через дополнительный SDK-метод `Status() sdktrace.Status`. Корректный wrapper, реализующий только публичный `trace.Span`, этого метода не имеет. В таком случае вызывается `SetStatus(Ok)`, даже если span уже помечен Error. `FromSDK` разрешает такой tracer/provider.

**Воспроизведение:** обернуть SDK span публичным интерфейсом; `StartSpan` → `SetStatus(Error, "failed")` → возвращённый `SpanEnd`. Экспортированный статус: `{Ok }`. Ошибка операции исчезает из outcome telemetry.

**Исправить:** generic End не должен выводить успех из отсутствия доступного getter. Оставлять Unset без явно известного результата, сохранять установленный Error. Убрать зависимость корректности от SDK-only introspection. Если нужен явный success-helper, определить его как отдельное действие с понятным владением outcome.

**AAA:** реальный SDK, wrapper только публичного интерфейса и noop; установленный Error не заменяется Ok; простое завершение не обещает неизвестный успех. Проверить все вызовы общего helper, а не только Provider.StartSpan.

### MTR-R02 · P1 — release-скрипт захватывает посторонние untracked-файлы

**Код:** [scripts/release.sh:32](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/scripts/release.sh:32>), [scripts/release.sh:93](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/scripts/release.sh:93>).

Preflight через `git diff-index` проверяет tracked-изменения. Следующий `git add .` включает любые неигнорируемые untracked-файлы в release commit, на который создаются и публикуются теги. В синтетическом репозитории с `untracked-private-note.txt` preflight возвращает 0, а файл оказывается staged. Это условная утечка при наличии такого файла и запуске release; факта опубликованного секрета не установлено.

**Исправить:** проверять весь ожидаемый clean state; stage только явный список изменённых release manifests. Публиковать только созданные этим запуском теги вместо всех локальных tags. Не запускать реальную публикацию для приёмки.

**AAA:** временные local repository и bare remote; посторонний файл либо блокирует release, либо гарантированно отсутствует в commit; сторонний tag не уезжает; ожидаемые manifests и module tags согласованы. Failures не должны оставлять основной checkout в неожидаемом detached/dirty состоянии.

### MTR-R03 · P2 — ошибочная конфигурация превращается в успешный no-op

**Код:** [genai/recorder.go:157](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/genai/recorder.go:157>), [genai/tracker.go:48](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/genai/tracker.go:48>).

`RuntimeFromProvider` перехватывает любую ошибку `NewRuntimeFromProvider` и возвращает `NoopRuntime`. Теряются ошибки budget/profile/policy и создания instruments, хотя комментарий обещает fallback только для неполного provider setup.

**Воспроизведение:** валидный provider + `WithMaxContextLength(0)`. Строгий конструктор возвращает invalid-observation error; convenience API возвращает `genai.noopRuntime`. Дальнейшая работа может выглядеть нормальной при полном отсутствии GenAI exports.

**Исправить clear break:** удалить catch-all constructor, оставить constructor с error и явный `NoopRuntime`, выбираемый host. Обновить README и примеры. Автоматическое выключение telemetry при ошибке конфигурации не заменять предупреждением, которое тоже легко потерять. Намеренный noop/sampling оставить.

**AAA:** invalid budgets, profile/policy mismatch, instrument creation error видны caller; явный noop работает как задокументировано; optional telemetry включается/выключается в composition root.

### MTR-R04 · P2 — TraceSnapshot возвращает успех без восстановления trace

**Код:** [trace_snapshot.go:94](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/trace_snapshot.go:94>).

Snapshot всегда содержит W3C tracecontext, но restore использует произвольный propagator provider. Baggage-only propagator не понимает snapshot; метод возвращает `nil`, а valid trace отсутствует. При существующем trace caller может остаться с посторонним контекстом.

**Воспроизведение:** валидный snapshot + `FromSDK(... Propagator: propagation.Baggage{})`; результат `snapshot restore: false <nil>`.

**Исправить Contract-First:** отделить формат durable snapshot от network propagation. Декодировать собственный W3C формат независимо от transport; явно определить применение trust policy. Если восстановление запрещено — вернуть определённую ошибку, не подменять его успешным no-op. Не обходить запрет remote trace случайно ради исправления happy path.

**AAA:** W3C, baggage-only и иной propagator; пустой и существующий чужой trace; policy rejection. Успех означает совпадение восстановленной trace/span reference; rejection сохраняет исходный ctx и возвращает error.

### MTR-R05 · P2 — Enrich теряет корректные raw strings

**Код:** [enrich.go:41](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/enrich.go:41>), [enrich.go:65](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/enrich.go:65>).

Validation использует `baggage.NewMember`, принимающий percent-encoded value, а дальнейшая запись — `NewMemberRaw`. Из-за фильтрации до обновления provenance ledger значения с пробелом, `%`, Unicode и запятой исчезают из baggage, span и context logs.

**Воспроизведение:** `Enrich(ctx, StringAttribute("value", value))`; `simple` сохраняется, `hello world`, `100%`, `Косовский`, `a,b` дают пустой `BaggageMember`. Литерал `x%20y` проходит, что подчёркивает смешение raw и wire representation.

**Исправить:** единая validation raw-value; wire encoding выполнять на transport boundary. Не делать транспортные ограничения неявным запретом локальных observability values. Контракт [OTel NewMemberRaw](https://pkg.go.dev/go.opentelemetry.io/otel/baggage#NewMemberRaw) сверялся интеграционным ревьюером; pinned dependency source — основание для точных лимитов.

**AAA:** все указанные значения сохраняются в span/log/private context; разрешённый ingress/egress roundtrip возвращает исходную строку; буквальный `%20` не декодируется дважды; invalid keys по-прежнему обрабатываются согласно контракту.

### MTR-R06 · P2 — GenAI WithScope обходит обработку metadata

**Код:** [genai/scope_context.go:17](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/genai/scope_context.go:17>), [genai/reference_policy.go:33](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/genai/reference_policy.go:33>), [enrich.go:34](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/enrich.go:34>).

Обе формы `WithScope` вызывают trusted raw `Enrich`; метод Tracker игнорирует receiver/config. Scope metadata оказывается буквально в активном parent span и через private context может попасть в ContextHandler logs. Обычный GenAI metadata path требует safe-label declaration или raw opt-in, иначе fingerprint. Эти два контракта соединены неявно.

**Воспроизведение:** default provider, активный span, `genai.WithScope(ctx, Scope{Model:"private-model-123", Purpose:"secret-customer-123"})`; export содержит оба literal sentinel без raw policy. Это built-in helper path, не удалённый exploit и не утверждение, что любой прямой вызов trusted Enrich обязан санитизироваться.

**Исправить:** хранить исходные defaults приватно в context, применять tracker policy на каждой точке telemetry output. Явное raw enrichment оставить отдельным осознанным API. Не хешировать исходные defaults до merge: metadata consumers должны получать корректные оригинальные значения. Уточнить поведение scope вне tracker и провести чёткую границу между local trust и разрешением raw export.

**AAA:** direct Meta, package WithScope и Tracker.WithScope при default profile не выводят literal sentinel в parent/child spans и logs; safe-label/raw opt-in ведут себя согласованно; scope merge, local precedence и remote trust не сломаны.

### MTR-R07 · P3 — README задаёт противоречивую schema identity

**Код:** [README.md:617](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/README.md:617>), [genai/profile.go:7](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/genai/profile.go:7>).

Поздний раздел README называет `metry.genai.effective`; реализация и основной раздел документации используют `metry.genai.semconv.e07f4eb.v1`. Это противоречие текущего consumer contract.

**Исправить:** один нормативный schema ID; historical IDs только в явно исторической migration section. Объединить дописанный после License раздел с основным описанием effective contract. Не добавлять отдельную подсистему генерации документации ради одной строки.

**Приёмка:** все нормативные примеры и профиль используют exported identity; conformance example проходит на действующей реализации.

### MTR-R08 · P3 — aborted stream ошибочно приравнен к неизвестному TTFT

**Код:** [genai/recorder.go:45](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/metry/genai/recorder.go:45>).

Комментарий `Unknown/aborted streams leave FirstTokenPresent false` советует потерять известное измерение, если stream прервался уже после первого токена. Реализация допускает известный TTFT при failure/cancellation.

**Исправить:** false означает, что первый токен не наблюдался или timing неизвестен. Ошибка после первого токена не отменяет measurement; отсутствие final usage — отдельное состояние.

**AAA:** first token в 20ms, cancellation в 100ms, final usage отсутствует: TTFT экспортирован, usage не выдуман. Cancellation до первого токена не экспортирует TTFT и usage.

## Отдельный реестр странных решений

Ниже не дополнительные доказанные production bugs. Для каждого пункта нужно выполнить указанное решение либо зафиксировать аргументированное отклонение с конкретным consumer contract. Отсутствие ошибки в тестах само по себе не является обоснованием сохранить лишний API.

| ID | Наблюдение и код | Предлагаемое решение |
|---|---|---|
| D01 | `Profile` смешивает schema descriptor и runtime config; ID/mapping/fields/boundaries изменяемы, но Validate разрешает только фиксированный каталог. `genai/profile.go:68,156,222,250` | Разделить immutable descriptor и действительно настраиваемую policy/budget/dimensions. Сохранить introspection/conformance; не превращать это в универсальный registry схем. |
| D02 | init bridge через глобальные functions с `any`, хотя уже есть typed `Provider.SDK()`. `wire_init.go:14`, `genai/tracker.go:55`, `sdk.go:55` | Удалить избыточный MeterTracer bridge, если typed dependency path сохраняет точные scope/schema/version. AsyncHandle representation оставить private; не раскрывать внутренности только ради удаления bridge. |
| D03 | `Runtime` — интерфейс instrumentation, не execution; крупная общая surface. `genai/recorder.go:102` | Предпочесть `Recorder`/`Instrumentation`, consumer-owned узкие interfaces. Если сохранить Runtime, явно обосновать naming и разграничить с harness. Не строить новую обязательную иерархию interfaces. |
| D04 | `RecordStreamingUpdate` валидирует и отбрасывает DTO; README честно это описывает. `genai/operation_handle.go:274` | Переименовать в ValidateStreamingUpdate либо удалить в пользу нужного consumer validation API. Не добавлять interim accounting или inferred final usage ради оправдания названия. |
| D05 | Scalar `RecordTTFT` молча отбрасывает zero/negative и не возвращает error, typed final path сохраняет known zero. `genai/metrics.go:13,75` | Предпочесть один presence-aware timing contract. Если необходим ранний TTFT export — typed API с error и single-owner правилом относительно Finish. Текущий README уже предупреждает о double counting; самого скрытого double counting не доказано. |
| D06 | WithMaxContextLength — лимит payload bytes, не model context tokens. `genai/config.go:117,126` | Переименовать в WithPayloadByteLimit / WithEventTextByteLimit; отдельно оценить wire/schema compatibility затронутых имён. |
| D07 | `DoctorID`/`PatientID` в generic core. `attribute.go:149` | Удалить в clear break; перенести helpers в соответствующий сервис. `StringAttribute` достаточно. Общие TenantID/SubjectID не удалять механически. |
| D08 | `NoopAsyncHandle` создаёт фиксированные валидные trace/span IDs. `internal/async/handle.go:42` | Явное disabled representation, не способное создавать фиктивные causal links при передаче recording provider. Или удалить helper. Synthetic IDs нельзя использовать как evidence реального origin. |
| D09 | `LinkedSpanWriter.SetError` замыкает переменную без синхронизации; nil очищает прежний error. `async_handle.go:161` | Определить concurrency/lifetime и nil contract. Предпочтительно защитить shared state и не сбрасывать уже известную ошибку случайным nil; при поддержке concurrent callback workers добавить race test. Без контракта concurrent use не объявляется уже доказанной поддерживаемой поломкой. |
| D10 | Metric maps для histogram/counter/gauge заполняются, но не читаются; отдельный names registry уже проверяет коллизии. `metrics_registry.go:20` | Удалить избыточные retained maps; не добавлять lookup API без реального сценария. |
| D11 | Shutdown terminal через sync.Once, deadline 10s, первый отменённый вызов окончателен. `metry.go:185` | Документировать terminal/idempotent semantics, живой shutdown ctx и ожидание concurrent callers. Не вводить ложные retries/reopen поверх terminal SDK shutdown. Убрать описание Provider как stateless. |
| D12 | Baggage allowlist без частного `metry.attr.type` не принимает обычный W3C string member. `propagation/policy.go:127` | Явно выбрать interoperability contract. Предпочтительно допускать allowlisted member без marker как string, сохранив private provenance и local-wins. Если marker обязателен — документировать wire пример и причину. |
| D13 | Ошибка `baggage.New` при превышении лимитов игнорируется; pinned SDK может вернуть произвольное подмножество из map. `propagation/policy.go:94` | Задать детерминированное bounded retention с понятным порядком и bounded diagnostics. Не строить отдельную quota platform. Проверить byte/member limits; это потеря данных при overflow, не обход trust. |
| D14 | Паника custom PayloadPolicy после `ToolHandle.finished=true` может оставить span незавершённым; diagnostic callback защищён иначе. `genai/tool.go:119`, `genai/helpers.go:241` | Задать общий callback failure contract и гарантировать завершение span, даже если panic сохраняется для host. Recovery не должен выдавать success или логировать raw panic. Это сценарий неисправного host callback, не дефект встроенной policy. |
| D15 | EvaluationAssociation требует полный experiment tuple даже для частичной online provenance. `genai/evals.go:103` | Сверить с реальным online consumer evaly. Если grader/target нужны без experiment — отделить independent provenance от полного experiment association. Не выдумывать dataset/trial IDs и не реализовывать eval runner в metry. |
| D16 | BSD-only release edits, detached HEAD без cleanup trap. `scripts/release.sh:78,86` | Portable manifest edits, isolated release checkout и документированное восстановление после failure. Проверить на поддерживаемых платформах. Development v0.0.0/replace не объявлять ошибкой: скрипт их намеренно переписывает. |

## Границы ответственности

Metry владеет observability context, instrumentation lifetime, spans/metrics/log correlation, bounded export, privacy telemetry, units/presence и проверяемым mapping. Identity и факты приходят от caller. Прямых обязательных зависимостей от domain DTO соседних AI-библиотек в рассмотренном GenAI core не найдено; сохранить BYOT через небольшие adapter contracts.

StartAgent/StartWorkflow и operation handles записывают интервалы, а не запускают агента или workflow. EvaluationRecorder принимает готовый результат, а не дублирует evaly. Security observers записывают принятое решение, а не дублируют guardy enforcement. Conformance fixtures проверяют instrumentation, а не проводят оценку качества моделей. Эти части не требуют удаления из metry только по названию.

За пределами библиотеки остаются agent loop, Ask/Plan/Action, retry/scheduler, product UI, pricing lookup/conversion, billing/quota enforcement, grader execution, experiment orchestration и transcript storage. Новой библиотеки для исправлений из этой задачи не требуется. Медицинские attribute helpers — конкретное исключение, которое нужно вынести в consumer.

Сохранить оправданные fallback-поведения: намеренный Noop, unknown при отсутствии measurement, safe fingerprints/ограниченные dimensions, default redaction. Убирать скрытое подавление configuration errors, фиктивную causal identity и необъяснённую потерю данных. Fail-closed sanitation не является ненужным fallback сама по себе.

## Документация и доказательство интеграции

- [x] Сохранить полезные разделы README/INTEGRATION/PRIVACY и GenAI CONTRACT/SEMANTICS/MIGRATION; сделать README навигационным, без дублирования task history и нормативных дописок после License.
- [x] Ссылки `.cursor/docs/task14...`, task16 и смешанный task-language в `genai/OUTCOMES.md` заменить стабильными consumer-facing материалами с явной версией контракта. Историю при необходимости оставить в отдельном разделе.
- [x] Описать поддерживаемую concurrency Runtime/Tracker/handles/callbacks, lifetime ошибок и методов после Finish/Shutdown, caller-owned SDK shutdown, nil/noop semantics. Примеры должны соответствовать выбранному контракту, а не обещать неподтверждённую thread safety.
- [x] В privacy matrix отдельно показать GenAI metadata, direct trusted enrichment, exception diagnostics и обычные HTTP path/user-agent channels. Default diagnostic sanitation не означает автоматическую санацию каждого HTTP-атрибута.
- [x] Уточнить границы conformance evidence. `integration/otlp/otlp_test.go:100` проверяет реальные wire names, но сам по себе не доказывает numeric/status/privacy semantics.
- [x] Добавить компактный реальный OTLP semantic fixture: resource/scope/schema, known-zero vs unknown usage, histogram/cost values, causal parent/link, error outcome и absence of raw sentinel в сериализованных сигналах. Не копировать всю unit suite в transport test.
- [x] Документировать release workflow, поддерживаемые платформы и recovery. Исправить R07/R08 и обновить все переименованные API в examples/migration.

## Порядок реализации и готовность

1. Зафиксировать новые публичные контракты status/restore/scope/enrichment и список breaking changes. Для D01–D16 записать окончательное решение и краткое обоснование; условные изменения вроде D15 привязать к настоящему consumer scenario.
2. Закрыть R01–R06 с независимыми AAA regression cases; исправить release staging до любых публикаций. Не ограничиваться переписыванием документации поверх воспроизведённых дефектов.
3. Выполнить согласованное API cleanup. Удалять legacy wrappers, obsolete tests/docs и лишний retained state; не поддерживать старые некорректные semantics ради совместимости.
4. Закрыть R07/R08, обновить migration и consumer examples, добавить компактный wire fixture.
5. Выполнить `make test`, `make lint`, `make test-examples` во всех модулях; targeted race tests для concurrency contracts. Release проверить только в изолированных temporary repositories, без production push. Дополнительные benchmarks нужны лишь для изменений hot path.

**Definition of Done:** все восемь R-пунктов закрыты; по каждому D есть реализованное решение либо конкретно обоснованное сохранение; тесты воспроизводят прежние нарушения и подтверждают новое поведение; docs и API едины; нет новых execution/domain dependencies; миграция честно перечисляет removals/renames/semantic changes. Успешный lint и старые тесты не заменяют regression evidence.


## Реализация и итоговая проверка (2026-10-06)

T01–T13 выполнялись последовательно. Каждая задача принята отдельными
независимыми субагентами по полноте (100%) и корректности (без неустранённых
замечаний), после чего сделан отдельный короткий commit. Решения D01–D16,
breaking changes, результаты повторных приёмок и ограничения доказательств
зафиксированы в [реестре выполнения](task23-progress.md).

| Требование | Реализация и первичное доказательство |
|---|---|
| R01 | T02: `internal/traceutil/span.go`, `provider_span_status_test.go`; публичный wrapper сохраняет Error, неизвестный outcome остаётся Unset. |
| R02, D16 | T03: `scripts/release.sh`, `scripts/release_test.sh`, `scripts/RELEASE.md`; isolated checkout, explicit manifests/tags, failure cleanup; тестовые local bare remotes. |
| R03 | T04: `genai/recorder.go`, `genai/constructor_contract_test.go`; ошибки construction видны host, явный NoopRecorder. |
| R04 | T05: `trace_snapshot.go`, `trace_snapshot_test.go`; durable W3C restore независимо от transport, policy rejection сохраняет исходный context. |
| R05, D12–D13 | T06: `enrich.go`, `enrichment_raw_contract_test.go`, `propagation/policy.go`, `internal/baggageutil/bounded_test.go`; raw/local/wire matrix, обычный W3C ingress, deterministic bounds. Hot-path benchmark tradeoff записан в реестре. |
| R06 | T07: `genai/scope_context.go`, `genai/scope_privacy_test.go`; private merge до policy, entry-point/policy privacy matrix. |
| D01–D06 | T08: `genai/schema.go`, `genai/contract.go`, `genai/recorder.go`, `genai/MIGRATION.md`; fixed immutable descriptor, typed SDK, Recorder, validation-only streaming, один TTFT owner, byte limits. |
| D07–D11 | T09: core helpers/registry/async/shutdown; `async_handle_contract_test.go`, `provider_shutdown_contract_test.go`, `genai/disabled_async_contract_test.go`, `internal/async/disabled_test.go`. |
| D14 | T10: `genai/callback_failure.go`, `genai/callback_failure_test.go`; owned spans завершаются при host panic, raw panic не экспортируется, исходная panic передаётся caller. |
| D15 | T11: `genai/evals.go`, `genai/evaluation_provenance_test.go`, ready-grade BYOT adapter в `examples/genai_recorder`; реальные online consumer paths evaly указаны в реестре, новых domain dependencies нет. |
| R07–R08, документация | T12: README/INTEGRATION/PRIVACY и GenAI CONTRACT/SEMANTICS/MIGRATION/OUTCOMES; `genai/stream_contract_test.go` сохраняет 20ms TTFT после cancellation в 100ms без final usage. R08 был дефектом комментария: runtime уже поддерживал это поведение. |
| OTLP semantics | T13: `integration/otlp/semantic_test.go`; реальные serialized signals, resource/scope/schema, zero/unknown, numeric/status/privacy/causality. Ограничения fixture явно указаны в INTEGRATION. |

T14: `make test`, `make lint`, `make test-examples`, `make test-release`,
release suite на macOS Bash 3.2 и 5.3 и целевые race tests прошли на текущем
checkout. Логи: `/tmp/metry-t14-{test,lint,examples,release,release-bash32,release-bash5,targeted-race}.log`.
Linux release проверялся отдельно при T03; release scripts с тех пор не менялись.
Итоговая двойная приёмка всего DoD завершена: `t14_completeness` — 100%,
`t14_correctness` — без неустранённых замечаний. Независимые проверки и baseline
reproductions перечислены в реестре. Реальный release/push не запускался.

Это доказательства для указанной локальной реализации и controlled loopback OTLP
сценария. Внешние collector/backend и опубликованные артефакты не проверялись;
абсолютное отсутствие ошибок не заявляется.
