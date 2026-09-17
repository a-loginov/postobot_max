#!/usr/bin/env bash
# Развёртывание кабинета ПостоБот (мини-приложение MAX) на Linux-сервере.
#
# Запуск (из каталога html/):
#   sudo APP_DIR=/opt/postobot APP_URL=https://cab.example.ru bash deploy.sh
#
# Кабинет читает ту же БД, что и бот, поэтому APP_DIR должен совпадать
# с каталогом бота, где лежат .env и local_db/ (или настроен DATABASE_URL).
#
# Требуется: python3 + python3-venv + (опционально) Caddy для HTTPS.
set -euo pipefail

APP_DIR=${APP_DIR:-/opt/postobot}
APP_USER=${APP_USER:-postobot}
APP_URL=${APP_URL:-""}
WEB_PORT=${WEB_PORT:-8080}
SERVICE=postobot-web
SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="${APP_DIR}/html"
VENV="${WEB_DIR}/.venv"

echo "==> [1/6] Копирование кабинета в ${WEB_DIR}"
if ! id "${APP_USER}" >/dev/null 2>&1; then
    useradd --system --create-home --home-dir "${APP_DIR}" --shell /usr/sbin/nologin "${APP_USER}"
fi
install -d -m 755 -o "${APP_USER}" -g "${APP_USER}" "${WEB_DIR}" "${WEB_DIR}/uploads" "${WEB_DIR}/templates"
install -o "${APP_USER}" -g "${APP_USER}" "${SRC_DIR}/main.py"            "${WEB_DIR}/main.py"
install -o "${APP_USER}" -g "${APP_USER}" "${SRC_DIR}/requirements.txt"    "${WEB_DIR}/requirements.txt"
install -o "${APP_USER}" -g "${APP_USER}" "${SRC_DIR}/templates/"*.html   "${WEB_DIR}/templates/"

echo "==> [2/6] Виртуальное окружение + зависимости"
python3 -m venv "${VENV}" || true
"${VENV}/bin/pip" install --upgrade pip -q
"${VENV}/bin/pip" install -r "${WEB_DIR}/requirements.txt" -q

echo "==> [3/6] Проверка конфигурации"
if [ ! -f "${APP_DIR}/.env" ]; then
    echo "ВНИМАНИЕ: ${APP_DIR}/.env не найден. Скопируй конфиг бота (BOT_TOKEN,"
    echo "RESPONSIBLE_IDS, DB_PATH, MAT_WORDS, SPAM_WORD) в ${APP_DIR}/.env"
fi

echo "==> [4/6] systemd-юнит /etc/systemd/system/${SERVICE}.service"
cat > /etc/systemd/system/${SERVICE}.service <<EOF
[Unit]
Description=Postobot Web Cabinet (MAX mini-app)
Documentation=https://dev.max.ru/docs/webapps/bridge
After=network.target postobot.service

[Service]
Type=simple
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${WEB_DIR}
EnvironmentFile=${APP_DIR}/.env
Environment=APP_URL=${APP_URL}
ExecStart=${VENV}/bin/gunicorn -w 2 -k gthread --threads 4 -b 127.0.0.1:${WEB_PORT} --access-logfile - main:app
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload

echo "==> [5/6] HTTPS (требуется для мини-приложения MAX)"
reload_ok=false
if command -v caddy >/dev/null 2>&1 && [ -n "${APP_URL}" ]; then
    DOMAIN="${APP_URL#*://}"
    cat > /etc/caddy/Caddyfile <<EOF
${APP_URL} {
    reverse_proxy 127.0.0.1:${WEB_PORT}
}
EOF
    systemctl reload caddy 2>/dev/null || systemctl restart caddy 2>/dev/null || reload_ok=true
    echo "Caddy настроен на ${APP_URL} -> 127.0.0.1:${WEB_PORT}"
else
    reload_ok=true
fi
if [ "${reload_ok}" = "true" ]; then
    echo "Настрой TLS любым реверс-прокси (nginx/caddy/certbot), проксируя на 127.0.0.1:${WEB_PORT}."
fi

echo "==> [6/6] Запуск"
systemctl enable --now "${SERVICE}"
systemctl restart "${SERVICE}"
sleep 2
systemctl status "${SERVICE}" --no-pager | head -12 || true

echo
echo "Готово. Что дальше:"
echo "  - В .env кабинета используется тот же BOT_TOKEN, что и у бота."
echo "  - Укажи APP_URL (публичный https-адрес) в environment юнита:"
echo "      systemctl edit ${SERVICE}   # Environment=APP_URL=https://cab.example.ru"
echo "  - Открой приложение в MAX: https://max.ru/<bot_username>?startapp=cabinet"
echo "  - Логи: journalctl -fu ${SERVICE} -n 50"
echo "  - Без HTTPS мини-приложение не откроется на платформе MAX."
echo
if [ -n "${APP_URL}" ]; then
    echo "Проверка: curl -s -o /dev/null -w '%{http_code}\\n' ${APP_URL}/health"
fi