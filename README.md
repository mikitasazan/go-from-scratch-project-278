# Сокращатель ссылок (Go)

[![hexlet-check](https://github.com/mikitasazan/go-from-scratch-project-278/actions/workflows/hexlet-check.yml/badge.svg)](https://github.com/mikitasazan/go-from-scratch-project-278/actions)
[![checks](https://github.com/mikitasazan/go-from-scratch-project-278/actions/workflows/checks.yml/badge.svg)](https://github.com/mikitasazan/go-from-scratch-project-278/actions/workflows/checks.yml)

Сервис коротких ссылок: превращает длинный адрес в короткий код, ведёт по нему
на исходный адрес и записывает каждый переход.

Учебный проект Хекслета: https://ru.hexlet.io/programs/go-from-scratch

## Что умеет

- CRUD коротких ссылок через `/api/links`, с постраничной выдачей.
- Короткое имя можно задать самому или оставить пустым — сервер сгенерирует.
- `GET /r/:code` переводит на исходный адрес и записывает посещение: IP,
  User-Agent, Referer, код ответа.
- `GET /api/link_visits` отдаёт журнал посещений, тоже постранично.
- Веб-интерфейс из пакета Хекслета работает поверх этого API.

## Стек

- Go 1.26, [Gin](https://gin-gonic.com/) — HTTP-слой и валидация
  ([go-playground/validator](https://github.com/go-playground/validator))
- PostgreSQL, [pgx](https://github.com/jackc/pgx) — драйвер,
  [sqlc](https://sqlc.dev/) — запросы генерируются из SQL,
  [goose](https://github.com/pressly/goose) — миграции
- [Caddy](https://caddyserver.com/) — раздаёт статику и проксирует API
- [sentry-go](https://github.com/getsentry/sentry-go) — мониторинг ошибок,
  включается переменной `SENTRY_DSN`
- Docker, GitHub Actions, `golangci-lint`
- Фронтенд — пакет `@hexlet/project-url-shortener-frontend`

## API

| Метод | Путь | Что делает |
|---|---|---|
| `GET` | `/ping` | health-check, отвечает `pong` |
| `GET` | `/api/links` | список ссылок; `?range=[0,9]` — окно, обе границы включительно |
| `POST` | `/api/links` | создаёт ссылку; `short_name` не обязателен |
| `GET` | `/api/links/:id` | одна ссылка |
| `PUT` | `/api/links/:id` | меняет ссылку |
| `DELETE` | `/api/links/:id` | удаляет ссылку |
| `GET` | `/api/link_visits` | журнал переходов, с тем же `?range=` |
| `GET` | `/r/:code` | переход по короткой ссылке, `302` |

Списки отдают заголовок `Content-Range`, например `links 0-9/42`.

Ошибки приходят в одном виде: `400` c `{"error": "invalid request"}` на
сломанный JSON и `422` c `{"errors": {"<поле>": "<сообщение>"}}` на всё
остальное, включая занятое короткое имя.

## Установка

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

### Локально, в Docker — весь сервис одним контейнером

`Dockerfile` собирает фронтенд, бэкенд и goose, а Caddy внутри контейнера
раздаёт интерфейс и проксирует запросы в приложение:

```bash
docker network create ls-net
docker run -d --name ls-pg --network ls-net \
  -e POSTGRES_USER=links -e POSTGRES_PASSWORD=secret -e POSTGRES_DB=links \
  postgres:18-alpine
docker build -t link-shortener:local .
docker run -d --name ls-app --network ls-net -p 8090:80 \
  -e PORT=8080 -e BASE_URL=http://localhost:8090 \
  -e DATABASE_URL="postgres://links:secret@ls-pg:5432/links?sslmode=disable" \
  link-shortener:local
```

Интерфейс — http://localhost:8090/, API — там же под `/api/links`,
health-check — `curl http://localhost:8090/ping`.

### Разработка: фронтенд и бэкенд рядом

```bash
cp .env.example .env      # и поправить DATABASE_URL под свою базу
npm install
npm run dev               # бэкенд :8080 и фронтенд :5173 одной командой
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
