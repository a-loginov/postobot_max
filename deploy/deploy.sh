#!/usr/bin/env bash
# Развёртывание ПостоБот на Linux-сервере (systemd).
#
# Запуск (из корня проекта):
#   sudo APP_DIR=/opt/postobot APP_USER=postobot bash deploy/deploy.sh
#
# Требуется Go и Python3 на машине сборки. Бинарь собирается кросс-компиляцией
# под linux/amd64 (чистый Go, CGO не нужен).
set -euo pipefail

APP_DIR=${APP_DIR:-/opt/postobot}
APP_USER=${APP_USER:-postobot}
BIN_DIR="${APP_DIR}/bin"
SERVICE=postobot

echo "==> [1/5] Кросс-компиляция бинаря linux/amd64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/postobot .

echo "==> [2/5] Установка в ${APP_DIR}"
if ! id "${APP_USER}" >/dev/null 2>&1; then
    useradd --system --create-home --home-dir "${APP_DIR}" --shell /usr/sbin/nologin "${APP_USER}"
fi
install -d -m 755 "${APP_DIR}" "${BIN_DIR}" "${APP_DIR}/archive"
install -o "${APP_USER}" -g "${APP_USER}" bin/postobot "${BIN_DIR}/postobot"
install -o "${APP_USER}" -g "${APP_USER}" archive/export_month.py "${APP_DIR}/archive/export_month.py"
install -o "${APP_USER}" -g "${APP_USER}" archive/requirements.txt "${APP_DIR}/archive/requirements.txt"

echo "==> [3/5] Установка Python-зависимостей для архива"
pip3 install --break-system-packages -r "${APP_DIR}/archive/requirements.txt" 2>/dev/null \
    || pip3 install -r "${APP_DIR}/archive/requirements.txt"

echo "==> [4/5] Установка systemd-юнитов"
install -m 644 deploy/postobot.service         /etc/systemd/system/postobot.service
install -m 644 deploy/postobot-archive.service /etc/systemd/system/postobot-archive.service
install -m 644 deploy/postobot-archive.timer   /etc/systemd/system/postobot-archive.timer
systemctl daemon-reload

echo "==> [5/5] Запуск служб"
systemctl enable --now "${SERVICE}"
systemctl enable --now postobot-archive.timer
systemctl restart "${SERVICE}"

echo
echo "Готово. Дальше:"
echo "  1. Положи прод-confg в ${APP_DIR}/.env (см. .env.example):"
echo "     BOT_TOKEN, RESPONSIBLE_IDS, DATABASE_URL (postgres), ARCHIVE_DIR, MAT_WORDS"
echo "  2. Перезапуск:  systemctl restart ${SERVICE}"
echo "  3. Логи:        journalctl -fu ${SERVICE} -n 50"
systemctl status "${SERVICE}" --no-pager | head -15 || true