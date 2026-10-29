# Публикация backend за Caddy

## Что получится

- Caddy принимает HTTP/HTTPS на 80/443 и автоматически получает сертификат Let's Encrypt.
- Go API доступен через `https://api.example.com`.
- API-процесс не публикует порт наружу: он доступен только Caddy внутри Docker-сети.
- SQLite хранится в Docker volume `safe_route_data`.

## Требования

- Linux-сервер с Docker Engine и Docker Compose plugin.
- DNS `A`/`AAAA` запись `api.example.com` на IP сервера.
- Открытые TCP-порты 80 и 443.

## Установка

Из корня проекта скопируйте backend и deploy на сервер, затем:

```sh
cd deploy
cp ../backend/.env.production.example ../backend/.env.production
editor ../backend/.env.production
editor Caddyfile
```

В `Caddyfile` замените `api.example.com` на настоящий домен.

В `.env.production` обязательно задайте:

- длинный случайный `JWT_SECRET`;
- `AI_API_TOKEN` и точный `AI_MODEL`, если нужен удалённый AI-разбор;
- `CORS_ORIGINS` оставьте пустым для APK. Если добавляется Web-клиент, укажите только его origin через запятую, например `https://app.example.com`.

Не публикуйте `.env.production` и не добавляйте его в git.

Запуск:

```sh
docker compose up -d --build
docker compose ps
curl -f https://api.example.com/health
```

Логи:

```sh
docker compose logs -f api
docker compose logs -f caddy
```

Обновление:

```sh
git pull
docker compose up -d --build
```

Резервная копия SQLite:

```sh
docker run --rm \
  -v deploy_safe_route_data:/data:ro \
  -v "$PWD":/backup \
  alpine:3.22 \
  tar czf /backup/safe-route-data-$(date +%F).tar.gz -C /data .
```

Имя volume проверьте через `docker volume ls`; Compose обычно добавляет имя проекта как префикс.

## Сборка APK

APK — отдельный клиент. Backend не нужно встраивать в APK: приложение обращается к опубликованному HTTPS API.

```powershell
flutter pub get
flutter build apk --release `
  --dart-define=API_BASE_URL=https://api.example.com
```

Готовый файл:

```text
build/app/outputs/flutter-apk/release/app-release.apk
```

Его можно передать на Android-телефон и установить вручную. Для распространения через Google Play лучше использовать `flutter build appbundle --release` и подписанную production-сборку.

После изменения домена нужно пересобрать APK с новым `API_BASE_URL`; адрес API зашит в release-приложение.
