# 🚀 Руководство по публикации Tahr IDE, плагинов и бинарников на GitHub

Данный документ содержит пошаговую инструкцию по первоначальной публикации репозитория, обновлению реестра плагинов и выпуску скомпилированных бинарников IDE для Windows, Linux и macOS.

---

## 1. Первоначальная публикация репозитория на GitHub

### Шаг 1.1. Создайте пустой репозиторий на GitHub
1. Перейдите на [github.com/new](https://github.com/new).
2. Задайте имя: `tahr` (под аккаунтом `baibeicha`).
3. Выберите **Public**.
4. **Не** инициализируйте репозиторий файлами README, .gitignore или лицензией (они уже созданы локально).
5. Нажмите **Create repository**.

### Шаг 1.2. Подключите локальный репозиторий и отправьте код
Выполните в терминале в папке `d:\tahr`:

```bash
# 1. Привяжите удаленный репозиторий
git remote add origin https://github.com/baibeicha/tahr.git

# 2. Убедитесь, что основная ветка называется main
git branch -M main

# 3. Отправьте ветку и историю коммитов на GitHub
git push -u origin main
```

---

## 2. Публикация и обновление плагинов IDE

Плагины в Tahr IDE представляют собой компактные архивы формата Zstandard (`.tahr`). Встроенный маркетплейс и менеджер расширений загружают плагины напрямую через **GitHub Raw Content**:

```
https://raw.githubusercontent.com/baibeicha/tahr/main/plugins/<plugin-id>.tahr
```

### Шаг 2.1. Создание нового плагина
```bash
# Создать шаблон плагина
./bin/tahr plugin init my-awesome-plugin --lang=go
```
Это создаст папку с файлами `plugin.json`, `README.md` и кодом расширения.

### Шаг 2.2. Упаковка плагина в .tahr архив
```bash
# Упаковать исходники плагина в архив
./bin/tahr plugin pack ./plugins/my-awesome-plugin ./plugins/my-awesome-plugin.tahr
```

### Шаг 2.3. Добавление плагина в реестр `plugins/registry.json`
Откройте [plugins/registry.json](file:///d:/tahr/plugins/registry.json) и добавьте запись о вашем плагине:

```json
{
  "id": "my-awesome-plugin",
  "name": "My Awesome Plugin",
  "version": "1.0.0",
  "author": "baibeicha",
  "description": "Описание возможностей плагина",
  "category": "tools",
  "download_url": "https://raw.githubusercontent.com/baibeicha/tahr/main/plugins/my-awesome-plugin.tahr",
  "repo_id": "official-github",
  "repo_name": "Tahr Official",
  "tags": ["tools", "awesome"]
}
```

### Шаг 2.4. Загрузка плагина на GitHub
Поскольку плагины весят всего 1–3 КБ, они хранятся непосредственно в Git-репозитории в папке `plugins/`:

```bash
git add plugins/my-awesome-plugin/ plugins/my-awesome-plugin.tahr plugins/registry.json
git commit -m "feat(plugins): add my-awesome-plugin to official registry"
git push origin main
```

Сразу после выполнения `git push` плагин становится доступен пользователям по всему миру через окно **Extension Browser (Ctrl+,)**!

---

## 3. Публикация скомпилированных бинарников IDE (GitHub Releases)

Скомпилированные исполняемые файлы (~18 МБ) не следует коммитить напрямую в git-дерево, чтобы не раздувать историю. Их загружают в раздел **Releases**.

### Способ 1: Автоматический релиз через GitHub Actions (Рекомендуемый)

В репозитории уже настроен пайплайн [.github/workflows/release.yml](file:///d:/tahr/.github/workflows/release.yml).
Он автоматически собирает бинарники под 6 платформ, архивирует их и публикует релиз при пуше git-тега.

```bash
# 1. Создайте тег версии
git tag -a v0.1.0 -m "Release Tahr IDE v0.1.0"

# 2. Отправьте тег на GitHub
git push origin v0.1.0
```

GitHub Actions автоматически:
1. Скомпилирует `tahr` под Windows (amd64, arm64), Linux (amd64, arm64), macOS (Intel, Apple Silicon).
2. Сформирует архивы:
   - `tahr-windows-amd64.zip`
   - `tahr-windows-arm64.zip`
   - `tahr-linux-amd64.tar.gz`
   - `tahr-linux-arm64.tar.gz`
   - `tahr-darwin-arm64.tar.gz`
   - `tahr-darwin-amd64.tar.gz`
3. Создаст релиз **v0.1.0** и прикрепит к нему все архивы.

---

### Способ 2: Ручная локальная кросс-компиляция

Если вы хотите собрать бинарники на своем компьютере и загрузить их вручную:

#### Скрипт сборки в PowerShell:
```powershell
$version = "0.1.0"
New-Item -ItemType Directory -Force -Path "dist"

# Windows x64
$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -ldflags="-s -w -X main.Version=$version" -o "dist/tahr.exe" ./cmd/tahr
Compress-Archive -Path "dist/tahr.exe" -DestinationPath "dist/tahr-windows-amd64.zip" -Force
Remove-Item "dist/tahr.exe"

# Linux x64
$env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -ldflags="-s -w -X main.Version=$version" -o "dist/tahr" ./cmd/tahr
tar -czvf "dist/tahr-linux-amd64.tar.gz" -C dist tahr
Remove-Item "dist/tahr"

# macOS ARM64 (Apple Silicon)
$env:GOOS="darwin"; $env:GOARCH="arm64"; $env:CGO_ENABLED="0"
go build -ldflags="-s -w -X main.Version=$version" -o "dist/tahr" ./cmd/tahr
tar -czvf "dist/tahr-darwin-arm64.tar.gz" -C dist tahr
Remove-Item "dist/tahr"

Write-Host "Все архивы успешно собраны в папке dist/"
```

#### Загрузка через GitHub CLI (`gh`):
```bash
gh release create v0.1.0 dist/* --title "Tahr IDE v0.1.0" --notes "Initial public release of Tahr IDE"
```

#### Загрузка через веб-интерфейс:
1. Перейдите в ваш репозиторий: `https://github.com/baibeicha/tahr/releases`
2. Нажмите кнопку **Draft a new release**.
3. Введите тег: `v0.1.0`.
4. Перетащите мышкой файлы из папки `dist/` в область **Attach binaries by dropping them here**.
5. Нажмите **Publish release**.

---

## 4. Чек-лист перед каждым релизом

- [ ] Все тесты проходят успешно: `go test ./...`
- [ ] Бинарник компилируется без ошибок: `go build -o bin/tahr.exe ./cmd/tahr`
- [ ] Новые плагины упакованы в `plugins/<id>.tahr`
- [ ] Файл `plugins/registry.json` содержит актуальные версии плагинов и ссылки
- [ ] Версия в `cmd/tahr/main.go` соответствует номеру релиза
- [ ] Изменения зафиксированы в git и отправлены на GitHub: `git push origin main`
