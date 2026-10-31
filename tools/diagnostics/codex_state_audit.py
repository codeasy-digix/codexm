#!/usr/bin/env python3
import argparse
import json
import os
import re
import stat
import sys
from datetime import datetime, timezone
from pathlib import Path


SENSITIVE_HINTS = (
    "auth",
    "credential",
    "oauth",
    "token",
    "secret",
    "webhook",
    "api_key",
    "apikey",
)

COMPARE_FIELDS = (
    "kind",
    "mode",
    "size",
    "inode",
    "uid",
    "gid",
    "nlink",
    "mtime_ns",
    "ctime_ns",
    "birthtime_ns",
    "symlink_target",
)


def codex_home_from_env() -> Path:
    return Path(os.environ.get("CODEX_HOME", "~/.codex")).expanduser().resolve()


def audit_dir_for(home: Path) -> Path:
    return home / "auth-switch-audit"


def sanitize_label(label: str) -> str:
    label = label.strip() or "snapshot"
    label = re.sub(r"[^A-Za-z0-9._-]+", "-", label)
    return label.strip(".-") or "snapshot"


def iso_from_ns(ns):
    if ns is None:
        return None
    return datetime.fromtimestamp(ns / 1_000_000_000, tz=timezone.utc).astimezone().isoformat(timespec="seconds")


def classify(rel: str, kind: str) -> str:
    lower = rel.lower()
    parts = lower.split("/")
    name = parts[-1]
    if name.startswith("auth.json") or name == ".codex-auth-current-account":
        return "auth"
    if name == ".credentials.json" or name.startswith(".credentials.json."):
        return "mcp_oauth"
    if rel.startswith("cache/codex_apps_") or rel.startswith("cache/codex_app_directory/"):
        return "apps_cache"
    if rel.startswith("cache/remote_plugin_catalog/") or rel.startswith("plugins/cache/") or rel.startswith(".tmp/plugins/") or rel.startswith(".tmp/bundled-marketplaces/"):
        return "plugin_cache"
    if rel.startswith("browser/"):
        return "browser"
    if rel.startswith("computer-use/"):
        return "computer_use"
    if rel.startswith("process_manager/"):
        return "process"
    if name.endswith((".sqlite", ".sqlite-shm", ".sqlite-wal", ".db", ".db-shm", ".db-wal")):
        return "sqlite"
    if name in (".codex-global-state.json", ".codex-global-state.json.bak"):
        return "desktop_state"
    if name in ("config.toml", "hooks.json") and len(parts) <= 2:
        return "config"
    if name.endswith(".log") or "/log/" in lower:
        return "log"
    if "/sessions/" in lower or "/archived_sessions/" in lower:
        return "session"
    if "/cache/" in lower or "/vendor_imports/" in lower:
        return "cache"
    if "state" in lower or "history" in lower or "memories" in lower or "goals" in lower:
        return "state"
    if kind == "dir":
        return "dir"
    return "other"


def has_sensitive_hint(rel: str) -> bool:
    lower = rel.lower()
    return any(hint in lower for hint in SENSITIVE_HINTS)


def path_kind(st_mode: int) -> str:
    if stat.S_ISREG(st_mode):
        return "file"
    if stat.S_ISDIR(st_mode):
        return "dir"
    if stat.S_ISLNK(st_mode):
        return "symlink"
    if stat.S_ISSOCK(st_mode):
        return "socket"
    if stat.S_ISFIFO(st_mode):
        return "fifo"
    if stat.S_ISCHR(st_mode):
        return "char"
    if stat.S_ISBLK(st_mode):
        return "block"
    return "other"


def entry_for(path: Path, home: Path) -> dict:
    rel = path.relative_to(home).as_posix()
    try:
        st = path.lstat()
    except FileNotFoundError:
        return {
            "path": rel,
            "kind": "missing-during-scan",
        }

    kind = path_kind(st.st_mode)
    birthtime = getattr(st, "st_birthtime_ns", None)
    if birthtime is None and hasattr(st, "st_birthtime"):
        birthtime = int(st.st_birthtime * 1_000_000_000)

    entry = {
        "path": rel,
        "kind": kind,
        "category": classify(rel, kind),
        "sensitive_hint": has_sensitive_hint(rel),
        "mode": oct(stat.S_IMODE(st.st_mode)),
        "size": st.st_size,
        "inode": st.st_ino,
        "uid": st.st_uid,
        "gid": st.st_gid,
        "nlink": st.st_nlink,
        "mtime_ns": st.st_mtime_ns,
        "ctime_ns": st.st_ctime_ns,
        "birthtime_ns": birthtime,
        "mtime": iso_from_ns(st.st_mtime_ns),
        "ctime": iso_from_ns(st.st_ctime_ns),
        "birthtime": iso_from_ns(birthtime),
    }

    if kind == "symlink":
        try:
            entry["symlink_target"] = os.readlink(path)
        except OSError as exc:
            entry["symlink_target"] = f"<unreadable: {exc}>"

    return entry


def should_skip_dir(path: Path, audit_dir: Path) -> bool:
    try:
        path.relative_to(audit_dir)
        return True
    except ValueError:
        return False


def scan(home: Path) -> list[dict]:
    audit_dir = audit_dir_for(home).resolve()
    entries = []
    for root, dirs, files in os.walk(home, topdown=True, followlinks=False):
        root_path = Path(root)
        dirs[:] = sorted(
            d for d in dirs
            if not should_skip_dir((root_path / d).resolve(), audit_dir)
        )
        for name in dirs:
            entries.append(entry_for(root_path / name, home))
        for name in sorted(files):
            path = root_path / name
            if should_skip_dir(path.resolve(), audit_dir):
                continue
            entries.append(entry_for(path, home))
    entries.sort(key=lambda e: e["path"])
    return entries


def write_summary(snapshot: dict, path: Path) -> None:
    entries = snapshot["entries"]
    by_category = {}
    for entry in entries:
        by_category[entry["category"]] = by_category.get(entry["category"], 0) + 1

    interesting_categories = {
        "auth",
        "mcp_oauth",
        "config",
        "desktop_state",
        "apps_cache",
        "browser",
        "computer_use",
        "process",
        "sqlite",
        "state",
        "log",
    }
    interesting = [e for e in entries if e["category"] in interesting_categories]
    recent = sorted(
        entries,
        key=lambda e: max(e.get("mtime_ns") or 0, e.get("ctime_ns") or 0),
        reverse=True,
    )[:40]

    lines = [
        f"# Codex State Snapshot: {snapshot['label']}",
        "",
        f"- Created: {snapshot['created_at']}",
        f"- CODEX_HOME: `{snapshot['codex_home']}`",
        f"- Entries: {len(entries)}",
        "",
        "## Categories",
        "",
    ]
    for category, count in sorted(by_category.items()):
        lines.append(f"- `{category}`: {count}")

    lines.extend(["", "## Auth/Credential/State Candidates", ""])
    for entry in sorted(interesting, key=lambda e: (e["category"], e["path"])):
        lines.append(
            f"- `{entry['path']}` [{entry['category']}] size={entry.get('size')} "
            f"mtime={entry.get('mtime')} ctime={entry.get('ctime')}"
        )

    lines.extend(["", "## Most Recently Touched", ""])
    for entry in recent:
        lines.append(
            f"- `{entry['path']}` [{entry['category']}] size={entry.get('size')} "
            f"mtime={entry.get('mtime')} ctime={entry.get('ctime')}"
        )

    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def cmd_snapshot(args) -> int:
    home = Path(args.home).expanduser().resolve() if args.home else codex_home_from_env()
    if not home.is_dir():
        print(f"CODEX_HOME not found: {home}", file=sys.stderr)
        return 1

    audit_dir = audit_dir_for(home)
    audit_dir.mkdir(parents=True, exist_ok=True)

    label = sanitize_label(args.label)
    now = datetime.now(timezone.utc)
    stamp = now.strftime("%Y%m%dT%H%M%SZ")
    snapshot = {
        "schema": 1,
        "label": label,
        "created_at_utc": now.isoformat(timespec="seconds"),
        "created_at": now.astimezone().isoformat(timespec="seconds"),
        "codex_home": str(home),
        "entries": scan(home),
    }

    snapshot_path = audit_dir / f"{stamp}_{label}.snapshot.json"
    summary_path = audit_dir / f"{stamp}_{label}.summary.md"
    snapshot_path.write_text(json.dumps(snapshot, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    write_summary(snapshot, summary_path)

    print(f"snapshot={snapshot_path}")
    print(f"summary={summary_path}")
    print(f"entries={len(snapshot['entries'])}")
    return 0


def load_snapshot(path: Path) -> dict:
    with path.open("r", encoding="utf-8") as fh:
        return json.load(fh)


def latest_snapshots(home: Path) -> list[Path]:
    audit_dir = audit_dir_for(home)
    return sorted(audit_dir.glob("*.snapshot.json"))


def changed_fields(old: dict, new: dict) -> dict:
    fields = {}
    for field in COMPARE_FIELDS:
        if old.get(field) != new.get(field):
            fields[field] = {"old": old.get(field), "new": new.get(field)}
    return fields


def format_change(change: dict) -> str:
    fields = ", ".join(change["fields"].keys())
    old = change.get("old", {})
    new = change.get("new", {})
    old_time = old.get("ctime") or old.get("mtime")
    new_time = new.get("ctime") or new.get("mtime")
    return (
        f"- `{change['path']}` [{new.get('category') or old.get('category')}] "
        f"fields={fields} old_ctime={old_time} new_ctime={new_time}"
    )


def cmd_diff(args) -> int:
    home = Path(args.home).expanduser().resolve() if args.home else codex_home_from_env()
    if args.old and args.new:
        old_path = Path(args.old).expanduser().resolve()
        new_path = Path(args.new).expanduser().resolve()
    else:
        snaps = latest_snapshots(home)
        if len(snaps) < 2:
            print(f"Need at least two snapshots in {audit_dir_for(home)}", file=sys.stderr)
            return 1
        old_path, new_path = snaps[-2], snaps[-1]

    old_snapshot = load_snapshot(old_path)
    new_snapshot = load_snapshot(new_path)
    old_map = {entry["path"]: entry for entry in old_snapshot["entries"]}
    new_map = {entry["path"]: entry for entry in new_snapshot["entries"]}

    added = [new_map[p] for p in sorted(set(new_map) - set(old_map))]
    removed = [old_map[p] for p in sorted(set(old_map) - set(new_map))]
    changed = []
    for rel in sorted(set(old_map) & set(new_map)):
        fields = changed_fields(old_map[rel], new_map[rel])
        if fields:
            changed.append({
                "path": rel,
                "old": old_map[rel],
                "new": new_map[rel],
                "fields": fields,
            })

    category_priority = {
        "auth": 0,
        "mcp_oauth": 1,
        "config": 2,
        "desktop_state": 3,
        "apps_cache": 4,
        "browser": 5,
        "computer_use": 6,
        "process": 7,
        "sqlite": 8,
        "state": 9,
        "log": 10,
    }
    changed.sort(
        key=lambda c: (
            category_priority.get(c["new"].get("category") or c["old"].get("category"), 99),
            -(c["new"].get("ctime_ns") or 0),
            c["path"],
        )
    )

    now = datetime.now(timezone.utc)
    stamp = now.strftime("%Y%m%dT%H%M%SZ")
    audit_dir = audit_dir_for(home)
    audit_dir.mkdir(parents=True, exist_ok=True)
    diff_json_path = audit_dir / f"{stamp}_diff.json"
    diff_md_path = audit_dir / f"{stamp}_diff.md"

    payload = {
        "schema": 1,
        "created_at_utc": now.isoformat(timespec="seconds"),
        "old_snapshot": str(old_path),
        "new_snapshot": str(new_path),
        "added": added,
        "removed": removed,
        "changed": changed,
    }
    diff_json_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    lines = [
        "# Codex State Diff",
        "",
        f"- Old: `{old_path}`",
        f"- New: `{new_path}`",
        f"- Added: {len(added)}",
        f"- Removed: {len(removed)}",
        f"- Changed: {len(changed)}",
        "",
        "## Added",
        "",
    ]
    for entry in added[:200]:
        lines.append(f"- `{entry['path']}` [{entry['category']}] size={entry.get('size')} ctime={entry.get('ctime')}")
    if len(added) > 200:
        lines.append(f"- ... {len(added) - 200} more")

    lines.extend(["", "## Removed", ""])
    for entry in removed[:200]:
        lines.append(f"- `{entry['path']}` [{entry['category']}] size={entry.get('size')} ctime={entry.get('ctime')}")
    if len(removed) > 200:
        lines.append(f"- ... {len(removed) - 200} more")

    lines.extend(["", "## Changed", ""])
    for change in changed[:300]:
        lines.append(format_change(change))
    if len(changed) > 300:
        lines.append(f"- ... {len(changed) - 300} more")

    diff_md_path.write_text("\n".join(lines) + "\n", encoding="utf-8")

    print(f"diff_json={diff_json_path}")
    print(f"diff_summary={diff_md_path}")
    print(f"added={len(added)} removed={len(removed)} changed={len(changed)}")
    return 0


def cmd_list(args) -> int:
    home = Path(args.home).expanduser().resolve() if args.home else codex_home_from_env()
    for path in latest_snapshots(home):
        print(path)
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Snapshot and diff Codex home metadata without printing token contents.")
    parser.add_argument("--home", help="Codex home directory. Defaults to CODEX_HOME or ~/.codex.")
    subparsers = parser.add_subparsers(dest="command", required=True)

    snapshot_parser = subparsers.add_parser("snapshot", help="Create a metadata snapshot.")
    snapshot_parser.add_argument("label", nargs="?", default="snapshot")
    snapshot_parser.set_defaults(func=cmd_snapshot)

    diff_parser = subparsers.add_parser("diff", help="Diff two snapshots. Defaults to latest two.")
    diff_parser.add_argument("old", nargs="?")
    diff_parser.add_argument("new", nargs="?")
    diff_parser.set_defaults(func=cmd_diff)

    list_parser = subparsers.add_parser("list", help="List snapshots.")
    list_parser.set_defaults(func=cmd_list)

    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
