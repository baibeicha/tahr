# Экосистема плагинов и архитектура ядра Tahr (Dual-Target IDE)

> **Статус документа:** Мастер-спецификация экосистемы и план развития.  
> **Философия Tahr:** Молниеносный Headless Core на Go (Zero-CGO) с поддержкой двух фронтендов: терминального (**GoatUI TUI**) и нативного графического на GPU (**Gio GUI**).

---

## 1. Модули ядра: Core Primitives & Built-in Views (План реализации в Core)

Эти 9 компонентов работают непосредственно в конвейере ядра ([`internal/core`](file:///d:/tahr/internal/core)) и UI-слое ([`internal/ui/tui`](file:///d:/tahr/internal/ui/tui)), исключая FFI-оверхед, задержки ввода (< 1 мс) и падение 60–120 FPS.

| Модуль (ID) | Назначение и функционал | Реализация под капотом | Протоколы и зависимости |
|---|---|---|---|
| **`vim-mode`** | Модальный ввод (Normal, Insert, Visual). Движения (`h/j/k/l`, `w/b`, `f/t`), операторы (`d`, `c`, `y`) и текстовые объекты (`ciw`, `dap`, `y$`). | Детерминированный конечный автомат (FSM), перехватывающий ввод до буфера и транслирующий его в атомарные мутации Rope. | Внутреннее API перехвата клавиатурных событий ядра. |
| **`keymaps`** | Пресеты горячих клавиш VS Code, JetBrains и Sublime: мультикурсоры (`Ctrl+D`), дублирование строк (`Alt+Shift+Down`), комментирование (`Ctrl+/`). | Декларативный маппинг сочетаний клавиш в команды ядра. Хранит JSON/TOML-таблицы без жесткой привязки к коду. | `Internal Command Registry`. |
| **`auto-pairs`** | Автозакрытие парных символов (`()`, `{}`, `[]`, `""`, `''`). Удаление обоих символов по Backspace, пропуск закрывающего символа при вводе. | Хук на событие ввода символа (`OnCharInsert`): анализирует следующий символ в Rope и вставляет пару или сдвигает курсор с 0 аллокаций. | `Buffer Mutation API`. |
| **`snippets`** | Разворачивание сниппетов по Tab (шаблоны циклов, функций, структур) с навигацией по плейсхолдерам (`$1`, `$2`, `${0:default}`). | Парсер синтаксиса LSP Snippets TextMate. Управляет виртуальными маркерами табстопов внутри активного документа. Обеспечивает `snippetSupport: true` в LSP. | LSP Snippet Syntax, Buffer Markers API. |
| **`indent-guides`** | Вертикальные тонкие линии направляющих уровней вложенности блоков кода (с подсвечиванием текущего блока). | Расчет отступов строк в пробелах/табах в видимом вьюпорте; генерация виртуальных вертикальных линий в пустых позициях с 0 аллокаций. | Viewport Render Hook. |
| **`rainbow-delimiters`** | Раскраска вложенных парных скобок циклическими контрастными цветами по уровням глубины. | Однопроходный обход дерева скобок/токенов в видимом вьюпорте: расчет уровня вложенности узлов-разделителей (`(`, `)`, `{`, `}`) и наложение цветовых спанов. | AST & Token Depth Queries. |
| **`virtual-text-engine`** | Движок виртуального текста, Inlay Hints, Inline Blame и маскирования. | Слой виртуальных аннотаций поверх Rope-буфера: не меняет байты документа, отображает текст в конце строки или между рунами. | Inlay/Conceal Engine. Используется для LSP Inlay Hints, Git Inline Blame, DAP значений (`x = 42`) и скрытия `.env` секретов (`••••••••`). |
| **`search-in-files`** | Глобальный поиск и пакетная замена по всему проекту с поддержкой регулярных выражений, фильтров путей и `.gitignore`. | Фоновый параллельный обход директорий горутинами; динамическая подстановка несохраненных срезов текста из оперативной памяти открытых Rope-буферов. | Multi-threaded Directory Walker / Regex Engine, Workspace Search Modal. |
| **`problems-panel`** | Сводная панель ошибок компиляции, предупреждений и хинтов по всем открытым файлам проекта. | Прямая агрегация кэша диагностики из `internal/core/lsp` (`textDocument/publishDiagnostics`) с группировкой по файлам и мгновенным переходом по клику/Enter. | LSP Diagnostics Cache, Bottom Drawer UI. |

*(Примечание: в ядре Tahr уже функционируют: File Tree `tree.go`, PTY Terminal `terminal.go`, DAP HUD `dap_hud.go`, Git Diff Tracker `diff.go` и In-File Find/Replace `find_replace_modal.go`).*

---

## 2. Tier 1: Core & Code Essentials (13 плагинов — Релиз 1.0)

Фокус: Идеальное редактирование кода, скорость старта < 15 мс, полный цикл разработки и режим Dogfooding (разработка Tahr внутри Tahr).

| Плагин (ID) | Назначение и функционал | Реализация под капотом | Протоколы и зависимости |
|---|---|---|---|
| **`tahr-go`** | Поддержка Go: автодополнение, переход по F12, типы, запуск тестов, авто-форматирование (`gofumpt`), шаблоны модулей. | Декларативный `plugin.json` + фоновый процесс `gopls` + отладчик `dlv` (DAP). | Tree-sitter (WASM), LSP 3.17, Delve DAP. |
| **`tahr-rust`** | Поддержка Rust: автодополнение, типы, инспекция макросов, `cargo check`, авто-форматирование (`cargo fmt`). | Декларативный `plugin.json` + фоновый `rust-analyzer` + отладка через `lldb-dap`. | Tree-sitter (WASM), LSP 3.17, LLDB DAP. |
| **`tahr-python`** | Поддержка Python: автодополнение, виртуальные окружения (`.venv`), форматирование и линтинг (`ruff`). | Декларативный `plugin.json` + `pyright` / `ruff-lsp` + отладка `debugpy`. | Tree-sitter (WASM), LSP 3.17, debugpy DAP. |
| **`tahr-ts`** | Поддержка TypeScript и JavaScript: JSX/TSX, автодополнение, навигация, форматирование (`biome` / `prettier`). | Декларативный `plugin.json` + `typescript-language-server` + `vscode-js-debug`. | Tree-sitter (WASM), LSP 3.17, Node Debug DAP. |
| **`tahr-clangd`** | Поддержка C и C++: навигация по заголовкам, автодополнение, форматирование (`clang-format`). | Декларативный `plugin.json` + `clangd` + отладка `lldb-dap`. | Tree-sitter (WASM), LSP 3.17, LLDB DAP. |
| **`config-pack`** | Поддержка JSON, YAML, TOML, Markdown. Подсветка синтаксиса, навигация, валидация по схемам. | Декларативная регистрация грамматик Tree-sitter + кэш JSON-схем (SchemaStore) в оперативной памяти. | Tree-sitter (WASM), JSON Schema Validator (Go). |
| **`theme-pack`** | Цветовые темы: Catppuccin (Mocha, Macchiato, Frappe, Latte), Tokyo Night, Gruvbox, One Dark, Dracula. Импорт тем VS Code. | Парсер цветовых токенов TextMate; маппит скоупы на 24-битные RGB значения для GoatUI и GPU шейдеров Gio. | TextMate Theme JSON, Declarative Theme Engine. |
| **`git-lens`** | Автор и дата строки (Inline Blame), полосы изменений в Gutter (Add/Change/Del), интерактивный Side-by-Side Diff перед коммитом. | Потоковый вызов системного Git CLI через пул фоновых воркеров с троттлингом (500 мс) и кэшированием. 100% совместимость с LFS, SSH и submodules. | System Git CLI over `os/exec`, Virtual Text Engine. |
| **`test-runner`** | Поиск тестов в коде, кнопки Run / Debug / Bench над функциями (CodeLens), расстановка иконок статуса (✔/✖) в Gutter. | Анализ AST-дерева на тестовые сигнатуры (`Test*`, `#[test]`, `describe/it`); запуск через runner ядра и вывод в сплит. | CodeLens API, DAP Test Orchestrator. |
| **`test-coverage`** | Интерактивная подсветка покрытия кода тестами в Gutter (зеленый = покрыто, красный = не покрыто). | Парсинг отчетов покрытия (`go test -coverprofile`, `cargo tarpaulin`, LCOV) и наложение цветовых маркеров в Gutter. | Coverage Parser, Gutter Annotation API. |
| **`ai-completion`** | Локальные серые подсказки (Ghost Text) по нажатию Tab. Модель Qwen2.5-Coder-0.5B/1.5B (GGUF). Отклик 10–25 мс. | Мульти-провайдерный движок: локальный `llama-server` (Zero-CGO), внешний `Ollama` или Cloud API. Промпты FIM (Fill-in-the-Middle), дебаунс 150 мс. | FIM Protocol, HTTP Keep-Alive, llama-server / Ollama. |
| **`markdown-preview`** | Живой интерактивный предпросмотр Markdown в соседнем сплите с синхронным скроллингом. | Парсер CommonMark/GFM. В GoatUI — форматированный TUI-вывод с рамками таблиц; в Gio — векторная верстка с кликабельными ссылками. | GFM Parser, Dual-Target Split View. |
| **`bookmarks`** | Именованные и цифровые закладки по кодовой базе (`Ctrl+Shift+1..9`, `m a`) с быстрой навигацией. | Хранилище виртуальных маркеров, привязанных к строкам документов; сводная панель быстрого перехода. | Buffer Markers API, QuickPick Modal. |

---

## 3. Tier 2: Developer Productivity & APIs (11 плагинов — Реализованы и активны)

Фокус: Превращение Tahr в идеальную рабочую среду бэкендера, работа с микросервисными API, задачами и AI.

| Плагин (ID) | Назначение и функционал | Реализация под капотом | Протоколы и зависимости |
|---|---|---|---|
| **`ai-chat`** | Боковая панель AI-диалога (`Ctrl+L`): объяснение ошибок, рефакторинг кода, генерация тестов с контекстом файлов (`@file`, `@workspace`). | Sidecar-клиент к LLM-провайдерам (Claude, OpenAI, DeepSeek, Ollama) с потоковым SSE-рендерингом ответа в Markdown. | JSON-RPC 2.0 / SSE, Declarative Chat UI. |
| **`task-runner`** | Авто-обнаружение и запуск задач из `Makefile`, `Taskfile.yml`, `package.json`, `Justfile` в 1 клик во встроенном PTY-терминале. | Парсер файлов конфигурации задач; интеграция с системным терминалом Tahr для интерактивного выполнения. | Taskfile/Makefile Parsers, Terminal Integration. |
| **`rest-client`** | Запуск HTTP-запросов из файлов `.http` / `.rest` (заголовки, body, переменные среды), вывод ответа в соседний сплит. | Парсер спецификации RFC 7230 из текста буфера, отправка через Go `net/http` клиент с поддержкой TLS, streaming и таймингов. | `net/http`, Split View API. |
| **`grpc-proto`** | Подсветка `.proto`, автогенерация кода (`protoc` / `buf`), инспекция RPC-сервисов и генерация JSON-запросов. | Парсер формата `.proto`, опрос схемы сервиса, синтез моков запросов, интеграция с компилятором stubs. | `google.golang.org/grpc`, Protobuf AST. |
| **`devtools`** | Швейцарский нож бэкендера: декодер JWT, конвертер Unix Timestamp в дату, генератор UUID, Base64 encode-decode, SHA/MD5. | Всплывающая палитра быстрых утилит без выхода в веб-браузер. Чистый Go. | Pure Go Utilities, QuickPick Palette. |
| **`regex-tester`** | Интерактивная песочница отладки регулярных выражений с подсветкой групп совпадений на лету. | Модальное окно с полями ввода regex и тестового текста; вычисление совпадений движком Go `regexp` (RE2). | Go `regexp` Engine, Inlay Highlights. |
| **`go-generator`** | Специализированная кодогенерация для Go: теги структур (`json`, `db`, `yaml`), конструкторы `New...`, авто-реализация интерфейсов (`impl`). | Анализ AST Go через `go/parser` и `go/ast`; генерация кода и вставка через Buffer Mutation API. | `go/ast`, `go/format`, Buffer Mutation API. |
| **`git-graph`** | Интерактивное визуальное дерево коммитов и веток Git в отдельной вкладке-сплите. | Потоковый парсинг `git log --graph --oneline` с отрисовкой связей веток псевдографикой в TUI и векторными линиями в GUI. | System Git CLI, Declarative Canvas UI. |
| **`todo-tree`** | Поиск и группировка комментариев `TODO:`, `FIXME:`, `HACK:`, `BUG:` по всему проекту в отдельной древовидной панели. | Обращение к фоновому поисковому движку ядра с регулярным выражением; кэширование результатов при сохранении файлов. | Core Search API, Declarative TreeView. |
| **`sqlite-viewer`** | Мгновенное открытие и просмотр любых `.db` / `.sqlite` / `.sqlite3` файлов в проекте без настройки подключений. | Чтение файла базы данных через Pure-Go SQLite драйвер; отображение схемы и выборки в табличном DataGrid. | Pure Go SQLite, DataGrid UI. |
| **`docker-compose`** | Дерево контейнеров проекта из `docker-compose.yml`, статус, стриминг логов в сплит, запуск shell в 1 клик. | Парсинг YAML Compose файла, взаимодействие с Docker Engine через сокет или CLI. | Docker Engine REST API, Terminal Integration. |

---

## 4. Tier 3: Infrastructure, Data & Remote (5 плагинов — Реализованы и активны)

Фокус: Профессиональные инструменты для баз данных, анализа логов, блокнотов и распределенной командной разработки.

| Плагин (ID) | Назначение и функционал | Реализация под капотом | Протоколы и зависимости |
|---|---|---|---|
| **`log-viewer`** | Потоковый просмотрщик логов с кольцевым буфером, цветовой индикацией уровней и мгновенной фильтрацией. Real-time tailing (`tail -f`). | Быстрый кольцевой буфер `RingBuffer`, фоновый асинхронный tailer файлов, регулярные выражения. | RingBuffer, Stream Tailer, Viewport Filter. |
| **`db-inspector`** | Универсальный клиент баз данных (PostgreSQL, MySQL, MariaDB, SQLite) (`Ctrl+Alt+D`). Выполнение SQL-скриптов (`Ctrl+Enter`), схема таблиц и интерактивная сетка DataGrid. | Драйверы баз данных на чистом Go; рендеринг таблицы результатов с виртуальным скроллингом и пагинацией. | Pure Go database drivers, Declarative DataGrid. |
| **`db-er-diagram`** | Интерактивная 2D диаграмма отношений сущностей (ER Diagram) с алгоритмом авто-укладки Sugiyama и экспортом DDL/Mermaid. | Парсинг SQL-схемы, построение DAG-графа внешних ключей, ортогональная трассировка связей Манхэттен. | Sugiyama Layout Engine, 2D DAG Canvas. |
| **`jupyter-notebook`** | Интерактивный запуск и редактирование `.ipynb` блокнотов. Поддержка ячеек кода/markdown, rich текстового вывода и управления ядром. | Парсер спецификации Jupyter Notebook v4 JSON, локальное исполнение Python fallback и подключение к Jupyter REST API. | Jupyter Notebook v4 JSON, Python runner. |
| **`p2p-collab`** | Совместное редактирование кода в реальном времени без центрального сервера (P2P). LAN mDNS, Nostr WebSocket, BitTorrent DHT. | Прямое E2E шифрование, CRDT/OT синхронизация текста документов, отслеживание цветных курсоров коллег. | WebSockets, mDNS, Nostr, BitTorrent DHT, CRDT. |

---

## 5. Стандарты платформы и протоколы интеграции

### 5.1. Tree-sitter WASM: Batch Memory Span Protocol
* Грамматики компилируются в WASM (`wasm32-wasi`) и исполняются в рантайме Wazero (Zero-CGO).
* Инкрементальные дельты `ts_tree_edit` передаются в WASM.
* Запрос `highlights.scm` выполняется внутри WASM.
* Результат отдается единым плоским срезом памяти `[]Span{StartByte, EndByte, TokenType, Flags}` за один системный вызов `host.Memory().Read(...)`.

### 5.2. Декларативный UI (Headless Schema)
Плагины описывают интерфейс абстрактными схемами, независимыми от терминала и GPU:
* `TreeView`: иерархические деревья (таблицы, топики, контейнеры, коммиты).
* `DataGrid`: виртуальные таблицы с пагинацией и сортировкой для баз данных.
* `SplitView` / `MarkdownView`: разделенный просмотр ответов REST, логов, diff и документации.
* `FormView`: поля ввода параметров подключения и настроек.

### 5.3. IPC протокол Sidecar-процессов
* Все внешние сервисы и языковые сервера взаимодействуют с Tahr через **JSON-RPC 2.0 над stdin/stdout**.
* Защита от зомби-процессов: `Job Objects` на Windows (`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`) и `prctl(PR_SET_PDEATHSIG, SIGTERM)` на Linux.

### 5.4. Формат пакетов `.tahr`
* ZIP-архив со сжатием **Zstandard** (`klauspost/compress/zip`).
* Скорость распаковки с AVX2/NEON: 1.5–2 ГБ/сек на ядро.
* Модель «Распаковка при установке» в `~/.config/tahr/plugins/<plugin-id>/` с поддержкой симлинков (`tahr plugin link`) для мгновенного hot-reload.
