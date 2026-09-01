# Сокращатель ссылок (Go)

[![hexlet-check](https://github.com/mikitasazan/go-from-scratch-project-278/actions/workflows/hexlet-check.yml/badge.svg)](https://github.com/mikitasazan/go-from-scratch-project-278/actions)
[![checks](https://github.com/mikitasazan/go-from-scratch-project-278/actions/workflows/checks.yml/badge.svg)](https://github.com/mikitasazan/go-from-scratch-project-278/actions/workflows/checks.yml)

Спроектируйте приложение для удобных ссылок

Учебный проект Хекслета: https://ru.hexlet.io/programs/go-from-scratch


## Стек

- Go

## Установка

<!-- Опишите установку: клонирование, зависимости, переменные окружения -->

```bash
git clone https://github.com/mikitasazan/go-from-scratch-project-278.git
cd go-from-scratch-project-278
```

## Использование

### Локально, без Docker

```bash
make run
curl http://localhost:8080/ping   # pong
```

### Локально, в Docker — так же, как на хостинге

`Dockerfile` собирает бинарник и goose, а `bin/run.sh` накатывает миграции и
запускает приложение:

```bash
docker network create ls-net
docker run -d --name ls-pg --network ls-net \
  -e POSTGRES_USER=links -e POSTGRES_PASSWORD=secret -e POSTGRES_DB=links \
  postgres:18-alpine
docker build -t link-shortener:local .
docker run -d --name ls-app --network ls-net -p 8080:8080 \
  -e PORT=8080 \
  -e DATABASE_URL="postgres://links:secret@ls-pg:5432/links?sslmode=disable" \
  link-shortener:local
curl http://localhost:8080/ping   # pong
```

### Переменные окружения

| Переменная | Обязательна | Зачем |
|---|---|---|
| `PORT` | нет, по умолчанию `8080` | порт, на котором слушает сервис |
| `DATABASE_URL` | да, при запуске через `bin/run.sh` | строка подключения к PostgreSQL |
| `SENTRY_DSN` | нет | мониторинг ошибок; пустая переменная просто выключает его |

### Развёртывание на хостинге

Шаг проекта предлагал выложить сервис на Render и подключить мониторинг ошибок
в Bugsink. Этот шаг пропущен намеренно: проект ведётся как локальная разработка,
внешние аккаунты не заводились. Всё, что для деплоя нужно, в репозитории есть —
`Dockerfile` и `bin/run.sh` собираются и запускаются, что проверено локально
(команды выше). Код читает `SENTRY_DSN` из окружения, поэтому мониторинг
включается одной переменной, без правок кода.

---

<details>
<summary>Автоматические тесты Хекслета</summary>

Тесты запускаются на каждый коммит. За запуск отвечает файл `.github/workflows/hexlet-check.yml` — не удаляйте и не переименовывайте ни его, ни репозиторий.

</details>

## О Хекслете

[Хекслет](https://ru.hexlet.io/) — школа программирования: авторские программы обучения с практикой, поддержкой наставников и реальными проектами, которые остаются в резюме. Этот репозиторий — один из таких проектов.
