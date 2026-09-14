#!/usr/bin/env python3
"""Monthly archive export for ПостоБот.

At the end of each month this script:
  1. reads all requests of the month from the DB,
  2. writes them into an Excel file: archive/YYYY-MM.xlsx,
  3. removes the archived requests from the DB (the DB stays small).

Columns of the Excel file (mirror archive/archive.go):
  A: request_id   B: max_user_id   C: name   D: surname
  E: class        F: description   G: status H: created_at

Usage:
  export_month.py [--year 2025] [--month 8] [--db-driver sqlite|postgres]

Config via env (same as the bot):
  DATABASE_URL  — postgres DSN (takes priority)
  DB_PATH       — sqlite file path (default local_db/postobot.db)
  ARCHIVE_DIR   — archive folder (default archive/)
"""

import argparse
import calendar
import os
import sqlite3
import sys

try:
    import openpyxl
except ImportError:
    print("Install dependencies: pip install -r archive/requirements.txt", file=sys.stderr)
    sys.exit(1)


def _dsn(db_driver: str) -> str:
    url = os.environ.get("DATABASE_URL", "")
    if url:
        return url
    path = os.environ.get("DB_PATH", "local_db/postobot.db")
    if db_driver == "postgres":
        raise SystemExit("--db-driver postgres requires DATABASE_URL env")
    return path


def _connect(db_driver: str):
    if db_driver == "postgres":
        import psycopg2

        return psycopg2.connect(_dsn(db_driver))
    conn = sqlite3.connect(_dsn(db_driver))
    conn.row_factory = sqlite3.Row
    return conn


def fetch_rows(conn, start_ts: str, end_ts: str):
    if isinstance(conn, sqlite3.Connection):
        cur = conn.execute(
            """
            SELECT r.id, s.max_user_id, s.name, s.surname, s.class,
                   r.description, r.status, r.created_at
            FROM requests r
            JOIN students s ON s.id = r.student_id
            WHERE r.created_at >= ? AND r.created_at < ?
            ORDER BY r.created_at ASC
            """,
            (start_ts, end_ts),
        )
        return cur.fetchall()

    cur = conn.cursor()
    cur.execute(
        """
        SELECT r.id, s.max_user_id, s.name, s.surname, s.class,
               r.description, r.status, r.created_at
        FROM requests r
        JOIN students s ON s.id = r.student_id
        WHERE r.created_at >= %s AND r.created_at < %s
        ORDER BY r.created_at ASC
        """,
        (start_ts, end_ts),
    )
    return cur.fetchall()


def write_excel(path: str, rows) -> None:
    wb = openpyxl.Workbook()
    ws = wb.active
    ws.title = "Sheet1"
    ws.append(["request_id", "max_user_id", "name", "surname", "class",
               "description", "status", "created_at"])
    for r in rows:
        ws.append([r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7]])
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    wb.save(path)


def delete_rows(conn, request_ids):
    if not request_ids:
        return
    ids = ",".join(str(i) for i in request_ids)
    if isinstance(conn, sqlite3.Connection):
        conn.execute(f"DELETE FROM requests WHERE id IN ({ids})")
        conn.commit()
        return
    cur = conn.cursor()
    cur.execute(f"DELETE FROM requests WHERE id IN ({ids})")
    conn.commit()


def main() -> None:
    parser = argparse.ArgumentParser(description="Archive month requests to Excel")
    parser.add_argument("--year", type=int, default=None)
    parser.add_argument("--month", type=int, default=None)
    parser.add_argument("--db-driver", choices=["sqlite", "postgres"], default="sqlite")
    parser.add_argument("--dry-run", action="store_true", help="write Excel but keep DB rows")
    args = parser.parse_args()

    now = __import__("datetime").datetime.now()
    year = args.year if args.year else now.year
    month = args.month if args.month else now.month

    start = now.replace(year=year, month=month, day=1, hour=0, minute=0, second=0, microsecond=0)
    end = start.replace(month=start.month + 1) if start.month < 12 else start.replace(year=year + 1, month=1)
    start_ts = start.isoformat(sep=" ")
    end_ts = end.isoformat(sep=" ")

    conn = _connect(args.db_driver)
    try:
        rows = fetch_rows(conn, start_ts, end_ts)
        if not rows:
            print(f"No requests for {year:04d}-{month:02d}. Nothing to do.")
            return

        archive_dir = os.environ.get("ARCHIVE_DIR", "archive")
        path = os.path.join(archive_dir, f"{year:04d}-{month:02d}.xlsx")
        write_excel(path, rows)
        print(f"Archived {len(rows)} requests to {path}")

        if not args.dry_run:
            request_ids = [r[0] for r in rows]
            delete_rows(conn, request_ids)
            print(f"Deleted {len(request_ids)} requests from DB.")
    finally:
        conn.close()


if __name__ == "__main__":
    main()