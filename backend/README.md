# Safe Route Backend

Backend для приложения «Безопасный маршрут» на Go.

## Почему Go

Go выбран для API из-за быстрой компиляции, небольшого runtime, хорошей конкурентности и простого deployment одним бинарным файлом. Узкое место AI-запроса — сеть и upstream-модель, а не язык backend, поэтому Go даёт быструю обработку авторизации, SQLite и API-логики без усложнения Rust-экосистемой.

## Запуск

```powershell
Copy-Item .env.example .env
go mod tidy
go run ./cmd/server
```

Health check:

```text
GET http://localhost:8080/health
```

## API

Public:

```text
POST /api/v1/auth/register
POST /api/v1/auth/login
```

Authorized with `Authorization: Bearer <JWT>`:

```text
GET  /api/v1/me
PUT  /api/v1/me/preferences
GET  /api/v1/tasks?category=city&difficulty=easy&mode=choices
POST /api/v1/child/onboarding
POST /api/v1/child/join
POST /api/v1/child/analyze
GET  /api/v1/child/progress
POST /api/v1/parent/invites
GET  /api/v1/parent/activity
```

### Register

```json
{
  "login": "parent@example.com",
  "password": "at-least-8-chars",
  "role": "parent",
  "name": "Алексей"
}
```

### Parent invite

```json
{"expires_in_minutes": 10}
```

Ответ содержит одноразовый код. QR-код генерируется на frontend из этого кода.

### AI analysis

```json
{
  "task_id": 1,
  "answer": "Я не открою дверь и позвоню родителю",
  "mode": "free"
}
```

AI-ключ не попадает во Flutter. Backend использует `AI_API_URL`, `AI_API_TOKEN` и `AI_MODEL` из собственного `.env`. Если ключ или модель не указаны, используется безопасный локальный fallback.

`POST /api/v1/child/analyze/stream` принимает тот же запрос и возвращает SSE-события `delta`, затем `result` при успехе либо `error` при ошибке. HTTP 200 означает открытие потока; успешный разбор подтверждает только событие `result`. Backend поддерживает также обычный JSON-ответ от AI-шлюза, игнорирующего `stream: true`. Лимит анализа — 1024 токена. При обрыве по лимиту или фильтрации в серверном логе указывается `finish_reason`; такой ответ не засчитывается как выполненное задание.

## Production checklist

- заменить `JWT_SECRET` на случайный секрет из secret manager;
- запускать за HTTPS reverse proxy;
- использовать PostgreSQL вместо SQLite при нескольких инстансах;
- добавить rate limit и audit logging;
- ограничить CORS конкретными доменами;
- не логировать токены и ответы, содержащие персональные данные;
- добавить миграции через отдельный migration tool;
- вынести AI-запросы в очередь при высокой нагрузке.

## Кабинет разработчика

Доступен только при `APP_ENV=development`, непустом `DEV_ADMIN_LOGIN` и `DEV_ADMIN_PASSWORD` длиной от 12 символов. Реквизиты задаются только на сервере. После смены режима/реквизитов сервер нужно перезапустить.

```text
POST /api/v1/auth/admin                  # {login, password} -> JWT с ролью admin
GET  /api/v1/admin/tasks                 # список, включая correct_choice
POST /api/v1/admin/tasks                 # создание
PUT  /api/v1/admin/tasks/:id             # обновление на месте
```

Редактор передаёт поля `SafetyTask` и `correct_choice` (индекс безопасного варианта, начиная с 0). Для свободного ответа без вариантов поле равно `null`. Если варианты заданы, нужны 2–8 уникальных непустых строк и валидный безопасный индекс. `safety_tip` задаёт пример безопасных действий, используемый при AI-разборе. Детский `GET /tasks` не возвращает ключ `correct_choice`. Создание и обновление доступны только администратору; удаление и изменение исторических ответов детей не предусмотрены.

При первом обновлении существующая SQLite-база получает столбец `correct_choice`. Для существующих заданий с вариантами сохраняется прежний безопасный второй вариант. Версия начального наполнения фиксируется через `PRAGMA user_version`, поэтому переименованные задания не создаются повторно при перезапуске.

`GET /me` для ребёнка теперь содержит `child_profile` после знакомства (`name`, `age`, `parent_id`). `GET /child/progress` дополнен `completed_task_ids`; дни текущей недели нумеруются от 1 (понедельник) до 7 (воскресенье). Количества учитывают уникальные успешно решённые задания. Посещение сохраняется даже при разборе без зачёта. Попытка и активность сохраняются одной транзакцией.

`POST /child/join` требует созданный профиль ребёнка и погашает только переданный код атомарно. `GET /parent/activity` суммирует количества всех связанных детей и объединяет дни активности недели; серия — максимальная среди связанных детей. Тексты ответов, AI-разборы и ID решённых заданий в родительский ответ не входят.

Проверки: `go test ./...`. Интеграционные тесты используют отдельные временные SQLite-базы и не меняют `data/safe_route.db`.

## Цвет аккаунта

Авторизованные ребёнок и родитель сохраняют свой цвет через PUT /api/v1/me/preferences с JSON {"palette_index": 0}. Допустимы целые индексы 0–5 в порядке палитр Flutter. GET /api/v1/me возвращает palette_index, по умолчанию 0. Настройка хранится в таблице user_preferences, привязана к пользователю из JWT и переживает вход с другого устройства и перезапуск. Миграция добавляет таблицу без удаления данных. Для включения маршрута перезапустите бэкенд с новым кодом.

