from __future__ import annotations

import hashlib
import hmac
import json
import os
import re
import time
import uuid
from datetime import datetime, timedelta, timezone
from functools import wraps
from urllib.parse import unquote

import requests as httpclient
from flask import Flask, g, jsonify, render_template, request, send_from_directory
from werkzeug.utils import secure_filename

try:
    from dotenv import load_dotenv

    _proj_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    load_dotenv(os.path.join(_proj_root, ".env"))
except ImportError:
    pass

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.dirname(BASE_DIR)

BOT_TOKEN = os.getenv("BOT_TOKEN", "")
DB_PATH = os.getenv("DB_PATH", os.path.join(PROJECT_ROOT, "local_db", "postobot.db"))
RESPONSIBLE_IDS = [
    int(x) for x in os.getenv("RESPONSIBLE_IDS", "").split(",") if x.strip()
]
APP_URL = os.getenv("APP_URL", "").rstrip("/")
MAX_API = os.getenv("MAX_API", "https://platform-api2.max.ru")
AUTH_TTL = int(os.getenv("AUTH_TTL", "3600"))
MAX_UPLOAD_BYTES = int(os.getenv("MAX_UPLOAD_BYTES", str(8 * 1024 * 1024)))
ALLOWED_EXT = {"jpg", "jpeg", "png", "webp"}

# Списки из .env (совпадают со списками бота).
def _csv(name: str) -> list[str]:
    return [w.strip() for w in os.getenv(name, "").split(",") if w.strip()]

MAT_WORDS = _csv("MAT_WORDS")
SPAM_WORDS = _csv("SPAM_WORD")
DEFAULT_MAT = (
    "блядь,блять,хуй,хуя,пизда,пиздец,пидор,сука,ебал,ебать,ебан,"
    "нахуй,похуй,мудак,идиот,дурак,дебил,заебал,заеба"
)
if not MAT_WORDS:
    MAT_WORDS = [w.strip() for w in DEFAULT_MAT.split(",") if w.strip()]

SIM_THRESHOLD = 0.8
DUPLICATE_DAYS = 7

app = Flask(__name__)
app.config["UPLOAD_FOLDER"] = os.path.join(BASE_DIR, "uploads")
os.makedirs(app.config["UPLOAD_FOLDER"], exist_ok=True)


@app.get("/health")
def health():
    return jsonify({"ok": True})


@app.get("/uploads/<path:filename>")
def uploads(filename):
    return send_from_directory(app.config["UPLOAD_FOLDER"], filename)


# ---------------------------------------------------------------- max bridge auth

def validate_init_data(init_data: str) -> dict | None:
    """Проверяет WebAppData мини-приложения (HMAC-SHA256) и возвращает user.

    Алгоритм описан в https://dev.max.ru/docs/webapps/validation
    """
    if not init_data or not BOT_TOKEN:
        return None

    params: dict[str, str] = {}
    seen: set[str] = set()
    for pair in init_data.split("&"):
        key, _, value = pair.partition("=")
        if not key or key in seen:
            return None  # параметр обязан встречаться ровно один раз
        seen.add(key)
        params[key] = unquote(value)

    original_hash = params.pop("hash", None)
    if not original_hash:
        return None

    launch_params = "\n".join(f"{k}={params[k]}" for k in sorted(params))
    secret_key = hmac.new(b"WebAppData", BOT_TOKEN.encode(), hashlib.sha256).digest()
    signature = hmac.new(secret_key, launch_params.encode(), hashlib.sha256).hexdigest()
    if not hmac.compare_digest(signature, original_hash):
        return None

    try:
        auth_date = int(params.get("auth_date", "0"))
    except ValueError:
        return None
    if time.time() - auth_date > AUTH_TTL:
        return None

    try:
        user = json.loads(params.get("user", "{}"))
    except (ValueError, TypeError):
        return None

    if not isinstance(user, dict) or not user.get("id"):
        return None
    return user


def auth_required(f):
    @wraps(f)
    def wrapper(*args, **kwargs):
        user = validate_init_data(request.headers.get("X-Init-Data", ""))
        if not user:
            return jsonify({"error": "unauthorized"}), 401
        g.max_user = user
        return f(*args, **kwargs)

    return wrapper


# ---------------------------------------------------------------- db helpers

def connect():
    import sqlite3

    con = sqlite3.connect(DB_PATH)
    con.row_factory = sqlite3.Row
    con.execute("PRAGMA busy_timeout = 5000")
    con.execute("PRAGMA journal_mode = WAL")
    return con


def now_iso() -> str:
    return datetime.now().astimezone().isoformat(timespec="microseconds")


def find_or_create_student(max_uid: int) -> dict:
    con = connect()
    try:
        row = con.execute(
            "SELECT * FROM students WHERE max_user_id = ?", (max_uid,)
        ).fetchone()
        if row:
            return dict(row)
        cur = con.execute(
            "INSERT INTO students (max_user_id, created_at) VALUES (?, ?)",
            (max_uid, now_iso()),
        )
        con.commit()
        row = con.execute("SELECT * FROM students WHERE id = ?", (cur.lastrowid,)).fetchone()
        return dict(row)
    finally:
        con.close()


def normalize(text: str) -> str:
    out: list[str] = []
    prev_space = True
    for ch in text.lower():
        if ch.isspace() or not ch.isalnum():
            if not prev_space:
                out.append(" ")
                prev_space = True
        else:
            out.append(ch)
            prev_space = False
    return "".join(out).strip()


def collapse(s: str) -> str:
    """Убирает повторяющиеся подряд буквы: «череремша» → «черемша»."""
    out: list[str] = []
    prev = None
    for ch in s:
        if ch == prev:
            continue
        out.append(ch)
        prev = ch
    return "".join(out)


def _word_hit(words: list[str], lower: str, collapsed: str) -> bool:
    if not lower:
        return False
    for w in words:
        wl = w.lower()
        wc = collapse(wl)
        if lower.find(wl) >= 0 or lower.find(wc) >= 0:
            return True
        if collapsed.find(wc) >= 0:
            return True
    return False


def moderation(text: str) -> tuple[str, bool]:
    """Возвращает (причина отклонения, ок). Причина: '' | 'мат' | 'спам'."""
    lower = normalize(text)
    collapsed = collapse(lower)
    if _word_hit(MAT_WORDS, lower, collapsed):
        return "мат", True
    if _word_hit(SPAM_WORDS, lower, collapsed):
        return "спам", True
    return "", False


def tokens(s: str) -> list[str]:
    words: list[str] = []
    seen: set[str] = set()
    for w in s.split():
        if len(w) >= 3 and w not in seen:
            seen.add(w)
            words.append(w)
    return words


def shared_count(a: list[str], b: list[str]) -> int:
    set_b = set(b)
    return sum(1 for w in a if w in set_b)


def similarity(a: str, b: str) -> float:
    ta, tb = tokens(a), tokens(b)
    if not ta or not tb:
        return 0
    count = shared_count(ta, tb)
    return 2.0 * count / (len(ta) + len(tb))


def is_duplicate(a: str, b: str) -> bool:
    # At least two shared words: short terse comments ("тест", "стул")
    # are never flagged as duplicates, even when identical.
    ta, tb = tokens(a), tokens(b)
    if shared_count(ta, tb) < 2:
        return False
    return similarity(a, b) >= SIM_THRESHOLD


def find_duplicate(student_id: int, normalized: str) -> dict | None:
    con = connect()
    try:
        since = (datetime.now().astimezone() - timedelta(days=DUPLICATE_DAYS)).isoformat(timespec="microseconds")
        rows = con.execute(
            "SELECT * FROM requests WHERE student_id = ? "
            "AND status IN ('active','pending_moderation') AND created_at >= ?",
            (student_id, since),
        ).fetchall()
        normalized = normalize(normalized)
        for row in rows:
            if is_duplicate(normalized, row["normalized"] or ""):
                return dict(row)
        return None
    finally:
        con.close()


def save_photo(f) -> str | None:
    if not f or not f.filename:
        return None
    ext = os.path.splitext(f.filename or "")[1].lstrip(".").lower()
    if ext not in ALLOWED_EXT:
        return None
    f.seek(0, os.SEEK_END)
    size = f.tell()
    f.seek(0)
    if size > MAX_UPLOAD_BYTES:
        return None
    name = f"{int(time.time() * 1000)}_{uuid.uuid4().hex[:8]}.{secure_filename(f.filename).rsplit('.', 1)[-1]}"
    path = os.path.join(app.config["UPLOAD_FOLDER"], name)
    f.save(path)
    return f"/uploads/{name}"


# ---------------------------------------------------------------- notify responsible

def format_request(req: dict) -> str:
    status = "в работе"
    if req["status"] == "done":
        status = "✅ выполнено"
    photo = "нет"
    if req.get("photo_url"):
        photo = "приложено"
    return (
        f"🧾 Заявка №{req['id']}\n"
        f"👤 Кто запросил: {req['student_name'] or '—'} {req['student_surname'] or ''} "
        f"({req['student_class'] or '—'} класс)\n"
        f"🔧 Проблема: {req['description']}\n"
        f"📸 Фото: {photo}\nСтатус: {status}"
    )


def _buttons(request_id) -> list[dict]:
    return {
        "type": "inline_keyboard",
        "payload": {
            "buttons": [
                [
                    {"type": "callback", "text": "✅ Выполнено", "payload": f"done:{request_id}"},
                    {"type": "callback", "text": "⬆️ Важнее", "payload": f"up:{request_id}"},
                    {"type": "callback", "text": "⬇️ Ниже", "payload": f"down:{request_id}"},
                ]
            ]
        },
    }


def _photo_attachment(photo_url: str | None) -> dict | None:
    if not photo_url:
        return None
    if photo_url.startswith("http"):
        url = photo_url
    elif APP_URL:
        url = f"{APP_URL}{photo_url}"
    else:
        return None
    return {"type": "image", "payload": {"url": url}}


def notify_responsible(req: dict) -> None:
    """Отправляет заявку ответственному сотруднику через MAX Bot API."""
    if not BOT_TOKEN or not RESPONSIBLE_IDS:
        return
    attachments: list[dict] = []
    photo = _photo_attachment(req.get("photo_url"))
    if photo:
        attachments.append(photo)
    attachments.append(_buttons(req["id"]))

    body = {"text": format_request(req), "attachments": attachments}
    headers = {"Authorization": BOT_TOKEN}
    for resp_id in RESPONSIBLE_IDS:
        try:
            httpclient.post(
                f"{MAX_API}/messages",
                params={"user_id": resp_id},
                headers=headers,
                json=body,
                timeout=15,
            )
        except Exception as exc:  # noqa: BLE001
            app.logger.warning("notify responsible %s failed: %s", resp_id, exc)


# ---------------------------------------------------------------- api routes

@app.get("/api/me")
@auth_required
def api_me():
    user = g.max_user
    student = find_or_create_student(int(user["id"]))
    return jsonify(
        {
            "max_user_id": student["max_user_id"],
            "name": student.get("name") or user.get("first_name", ""),
            "surname": student.get("surname") or user.get("last_name", ""),
            "class": student.get("class") or "",
            "first_name": user.get("first_name", ""),
            "last_name": user.get("last_name", ""),
            "username": user.get("username", ""),
            "photo_url": user.get("photo_url") or "",
            "created_at": student.get("created_at"),
        }
    )


@app.post("/api/me")
@auth_required
def api_me_update():
    student = find_or_create_student(int(g.max_user["id"]))
    data = request.get_json(silent=True) or {}
    name = (data.get("name") or "").strip()
    surname = (data.get("surname") or "").strip()
    klass = (data.get("class") or "").strip()
    if not name:
        return jsonify({"error": "Имя не может быть пустым"}), 400
    con = connect()
    try:
        con.execute(
            "UPDATE students SET name = ?, surname = ?, class = ? WHERE id = ?",
            (name, surname, klass, student["id"]),
        )
        con.commit()
    finally:
        con.close()
    return jsonify({"ok": True, "name": name, "surname": surname, "class": klass})


@app.get("/api/requests")
@auth_required
def api_requests():
    student = find_or_create_student(int(g.max_user["id"]))
    con = connect()
    try:
        rows = con.execute(
            "SELECT * FROM requests WHERE student_id = ? ORDER BY created_at DESC",
            (student["id"],),
        ).fetchall()
    finally:
        con.close()
    items = []
    for r in rows:
        items.append(
            {
                "id": r["id"],
                "description": r["description"],
                "status": r["status"],
                "reject_reason": r["reject_reason"],
                "priority": r["priority"],
                "photo_url": r["photo_url"],
                "created_at": r["created_at"],
            }
        )
    return jsonify({"requests": items})


@app.post("/api/requests")
@auth_required
def api_requests_create():
    student = find_or_create_student(int(g.max_user["id"]))
    data = request.get_json(silent=True) or {}
    description = (request.form.get("description") or data.get("description") or "").strip()
    if not description:
        return jsonify({"error": "Опиши, что нужно заменить или починить"}), 400

    reason, bad = moderation(description)
    photo_url = None
    if "photo" in request.files:
        photo_url = save_photo(request.files["photo"])

    con = connect()
    try:
        if bad:
            cur = con.execute(
                "INSERT INTO requests (student_id, description, normalized, photo_url,"
                " status, reject_reason, created_at) VALUES (?,?,?,?,?,?,?)",
                (student["id"], description, normalize(description), photo_url,
                 "rejected", reason, now_iso()),
            )
            con.commit()
            rid = cur.lastrowid
            return jsonify(
                {
                    "ok": False,
                    "id": rid,
                    "status": "rejected",
                    "reject_reason": reason,
                    "message": "Заявка отклонена: сообщение содержит недопустимые выражения."
                    if reason == "мат"
                    else "Заявка отклонена: сообщение распознано как спам.",
                }
            ), 400

        dup = find_duplicate(student["id"], description)
        if dup:
            cur = con.execute(
                "INSERT INTO requests (student_id, description, normalized, photo_url,"
                " status, reject_reason, duplicate_of_id, created_at) VALUES (?,?,?,?,?,?,?,?)",
                (student["id"], description, normalize(description), photo_url,
                 "rejected", "дубликат", dup["id"], now_iso()),
            )
            con.commit()
            rid = cur.lastrowid
            return jsonify(
                {
                    "ok": False,
                    "id": rid,
                    "status": "rejected",
                    "reject_reason": "дубликат",
                    "message": "Похожая заявка уже отправлена и принята в работу. Дубликат не нужен.",
                }
            ), 400

        cur = con.execute(
            "INSERT INTO requests (student_id, description, normalized, photo_url,"
            " status, created_at) VALUES (?,?,?,?,?,?)",
            (student["id"], description, normalize(description), photo_url,
             "active", now_iso()),
        )
        con.commit()
        rid = cur.lastrowid
    finally:
        con.close()

    req = {
        "id": rid,
        "description": description,
        "photo_url": photo_url,
        "status": "active",
        "student_name": student.get("name"),
        "student_surname": student.get("surname"),
        "student_class": student.get("class"),
    }
    notify_responsible(req)

    return jsonify(
        {
            "ok": True,
            "id": rid,
            "status": "active",
            "photo_url": photo_url,
            "message": f"✅ Заявка №{rid} принята! Ответственный уже получил уведомление.",
        }
    )


@app.get("/")
def index():
    return render_template("my_accounts.html")


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=int(os.getenv("PORT", "8080")), debug=os.getenv("FLASK_DEBUG") == "1")