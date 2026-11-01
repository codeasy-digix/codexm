//! Metadata-only diagnostics. This module never reads a scanned file's contents,
//! follows a directory symlink, or changes native Codex data. It owns no daemon.
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::io;
use std::os::unix::fs::{FileTypeExt, MetadataExt};
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};
use time::{OffsetDateTime, format_description::well_known::Rfc3339};

const SENSITIVE: &[&str] = &[
    "auth",
    "credential",
    "oauth",
    "token",
    "secret",
    "webhook",
    "api_key",
    "apikey",
];
const COMPARE: &[&str] = &[
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
];

pub fn default_output(home: &Path, state: &Path) -> PathBuf {
    let hash = Sha256::digest(home.as_os_str().as_encoded_bytes());
    state.join("codexm/state-audit").join(format!("{hash:x}"))
}

pub fn sanitize_label(label: &str) -> String {
    let mut value = String::new();
    for ch in label.trim().chars() {
        if ch.is_ascii_alphanumeric() || "._-".contains(ch) {
            value.push(ch);
        } else if !value.ends_with('-') {
            value.push('-');
        }
    }
    let value = value.trim_matches(['.', '-']);
    if value.is_empty() {
        "snapshot".into()
    } else {
        value.chars().take(80).collect()
    }
}

pub fn classify(rel: &str, kind: &str) -> &'static str {
    let lower = rel.to_lowercase();
    let name = lower.rsplit('/').next().unwrap_or("");
    let has = |part: &str| lower.split('/').any(|p| p == part);
    if name.starts_with("auth.json") || name == ".codex-auth-current-account" {
        "auth"
    } else if name == ".credentials.json" || name.starts_with(".credentials.json.") {
        "mcp_oauth"
    } else if lower.starts_with("cache/codex_apps_")
        || lower.starts_with("cache/codex_app_directory/")
    {
        "apps_cache"
    } else if [
        "cache/remote_plugin_catalog/",
        "plugins/cache/",
        ".tmp/plugins/",
        ".tmp/bundled-marketplaces/",
    ]
    .iter()
    .any(|p| lower.starts_with(p))
    {
        "plugin_cache"
    } else if lower.starts_with("browser/") {
        "browser"
    } else if lower.starts_with("computer-use/") {
        "computer_use"
    } else if lower.starts_with("process_manager/") {
        "process"
    } else if [
        ".sqlite",
        ".sqlite-shm",
        ".sqlite-wal",
        ".db",
        ".db-shm",
        ".db-wal",
    ]
    .iter()
    .any(|p| name.ends_with(p))
    {
        "sqlite"
    } else if [".codex-global-state.json", ".codex-global-state.json.bak"].contains(&name) {
        "desktop_state"
    } else if ["config.toml", "hooks.json"].contains(&name) && lower.split('/').count() <= 2 {
        "config"
    } else if name.ends_with(".log") || has("log") {
        "log"
    } else if has("sessions") || has("archived_sessions") {
        "session"
    } else if has("cache") || has("vendor_imports") {
        "cache"
    } else if ["state", "history", "memories", "goals"]
        .iter()
        .any(|p| lower.contains(p))
    {
        "state"
    } else if kind == "dir" {
        "dir"
    } else {
        "other"
    }
}

fn timestamp(ns: i64) -> Option<String> {
    OffsetDateTime::from_unix_timestamp_nanos(ns.into())
        .ok()?
        .format(&Rfc3339)
        .ok()
}

fn system_nanos(value: SystemTime) -> Option<i64> {
    match value.duration_since(UNIX_EPOCH) {
        Ok(d) => i64::try_from(d.as_nanos()).ok(),
        Err(e) => i64::try_from(e.duration().as_nanos()).ok()?.checked_neg(),
    }
}

fn kind(meta: &fs::Metadata) -> &'static str {
    let t = meta.file_type();
    if t.is_file() {
        "file"
    } else if t.is_dir() {
        "dir"
    } else if t.is_symlink() {
        "symlink"
    } else if t.is_socket() {
        "socket"
    } else if t.is_fifo() {
        "fifo"
    } else if t.is_char_device() {
        "char"
    } else if t.is_block_device() {
        "block"
    } else {
        "other"
    }
}

fn entry(path: &Path, home: &Path) -> io::Result<Value> {
    let rel = path
        .strip_prefix(home)
        .map_err(io::Error::other)?
        .to_str()
        .ok_or_else(|| {
            io::Error::other("non-UTF-8 path cannot be represented without losing identity")
        })?;
    let lower = rel.to_lowercase();
    let meta = match fs::symlink_metadata(path) {
        Ok(m) => m,
        Err(e) if e.kind() == io::ErrorKind::NotFound => {
            return Ok(
                json!({"path":rel,"kind":"missing-during-scan","category":"other","sensitive_hint":SENSITIVE.iter().any(|h|lower.contains(h))}),
            );
        }
        Err(e) => return Err(e),
    };
    let kind = kind(&meta);
    let mtime = meta
        .mtime()
        .checked_mul(1_000_000_000)
        .and_then(|v| v.checked_add(meta.mtime_nsec()));
    let ctime = meta
        .ctime()
        .checked_mul(1_000_000_000)
        .and_then(|v| v.checked_add(meta.ctime_nsec()));
    let birth = meta.created().ok().and_then(system_nanos);
    let mut result = json!({"path":rel,"kind":kind,"category":classify(rel,kind),"sensitive_hint":SENSITIVE.iter().any(|h|lower.contains(h)),
        "mode":format!("0o{:o}",meta.mode()&0o7777),"size":meta.len(),"inode":meta.ino(),"uid":meta.uid(),"gid":meta.gid(),"nlink":meta.nlink(),
        "mtime_ns":mtime,"ctime_ns":ctime,"birthtime_ns":birth,"mtime":mtime.and_then(timestamp),"ctime":ctime.and_then(timestamp),"birthtime":birth.and_then(timestamp)});
    if kind == "symlink" {
        result["symlink_target"] = Value::String(
            fs::read_link(path)?
                .into_os_string()
                .into_string()
                .map_err(|_| {
                    io::Error::other(
                        "non-UTF-8 symlink target cannot be represented without losing identity",
                    )
                })?,
        );
    }
    Ok(result)
}

pub fn scan(home: &Path, output: &Path) -> io::Result<Vec<Value>> {
    let home = home.canonicalize()?;
    if !home.is_dir() {
        return Err(io::Error::other("Codex home must be a directory"));
    }
    let mut pending = vec![home.clone()];
    let mut entries = Vec::new();
    while let Some(dir) = pending.pop() {
        // Never descend a symlink, including one replacing an already queued dir.
        if !fs::symlink_metadata(&dir)?.is_dir() {
            continue;
        }
        for child in fs::read_dir(&dir)? {
            let path = child?.path();
            if path.starts_with(output) || path.starts_with(home.join("auth-switch-audit")) {
                continue;
            }
            let record = entry(&path, &home)?;
            if record["kind"] == "dir" {
                pending.push(path);
            }
            entries.push(record);
        }
    }
    entries.sort_by(|a, b| a["path"].as_str().cmp(&b["path"].as_str()));
    Ok(entries)
}

pub fn snapshot(home: &Path, output: &Path, label: &str) -> io::Result<Value> {
    let home = home.canonicalize()?;
    let now = OffsetDateTime::now_utc()
        .format(&Rfc3339)
        .map_err(io::Error::other)?;
    Ok(
        json!({"schema":2,"label":sanitize_label(label),"created_at_utc":now,"created_at":now,
        "codex_home":home,"entries":scan(&home,output)?}),
    )
}

fn index(snapshot: &Value) -> io::Result<BTreeMap<String, Value>> {
    if !matches!(snapshot["schema"].as_u64(), Some(1 | 2)) {
        return Err(io::Error::other("unsupported snapshot schema"));
    }
    let rows = snapshot["entries"]
        .as_array()
        .ok_or_else(|| io::Error::other("snapshot entries missing"))?;
    let mut result = BTreeMap::new();
    for row in rows {
        let path = row["path"]
            .as_str()
            .ok_or_else(|| io::Error::other("snapshot path missing"))?;
        if result.insert(path.to_owned(), row.clone()).is_some() {
            return Err(io::Error::other("duplicate snapshot path"));
        }
    }
    Ok(result)
}

pub fn diff(old: &Value, new: &Value) -> io::Result<Value> {
    if old["codex_home"].as_str().is_none() || new["codex_home"].as_str().is_none() {
        return Err(io::Error::other("snapshot Codex home is missing"));
    }
    if old["codex_home"] != new["codex_home"] {
        return Err(io::Error::other("snapshots refer to different Codex homes"));
    }
    let legacy = old["schema"] == 1 || new["schema"] == 1;
    let old = index(old)?;
    let new = index(new)?;
    let mut added = Vec::new();
    let mut removed = Vec::new();
    let mut changed = Vec::new();
    let paths: BTreeSet<_> = old.keys().chain(new.keys()).collect();
    for path in paths {
        match (old.get(path), new.get(path)) {
            (None, Some(after)) => added.push(after.clone()),
            (Some(before), None) => removed.push(before.clone()),
            (Some(before), Some(after)) => {
                let mut fields = serde_json::Map::new();
                for field in COMPARE {
                    // Python's macOS birthtime fallback was a floating point value.
                    if legacy && *field == "birthtime_ns" {
                        continue;
                    }
                    if before[field] != after[field] {
                        fields.insert(
                            (*field).into(),
                            json!({"old":before[field],"new":after[field]}),
                        );
                    }
                }
                if !fields.is_empty() {
                    changed.push(json!({"path":path,"old":before,"new":after,"fields":fields}));
                }
            }
            _ => unreachable!(),
        }
    }
    let priority = |v: &Value| {
        [
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
        ]
        .iter()
        .position(|c| v["new"]["category"] == *c)
        .unwrap_or(99)
    };
    changed.sort_by(|a, b| {
        priority(a)
            .cmp(&priority(b))
            .then_with(|| {
                b["new"]["ctime_ns"]
                    .as_i64()
                    .cmp(&a["new"]["ctime_ns"].as_i64())
            })
            .then_with(|| a["path"].as_str().cmp(&b["path"].as_str()))
    });
    Ok(
        json!({"schema":2,"created_at_utc":OffsetDateTime::now_utc().format(&Rfc3339).map_err(io::Error::other)?,"added":added,"removed":removed,"changed":changed,
        "compatibility_note":if legacy {"Legacy Python birthtime precision is ignored; all other metadata fields are compared."}else{""}}),
    )
}

fn text(v: &Value) -> &str {
    v.as_str().unwrap_or("unknown")
}
fn safe(v: &Value) -> String {
    text(v)
        .chars()
        .map(|c| if c.is_control() || c == '`' { ' ' } else { c })
        .collect()
}

pub fn snapshot_summary(snapshot: &Value) -> io::Result<String> {
    let rows = index(snapshot)?;
    let mut categories = BTreeMap::<String, usize>::new();
    for e in rows.values() {
        *categories.entry(text(&e["category"]).into()).or_default() += 1;
    }
    let mut out = format!(
        "# Codex State Snapshot: {}\n\n- Created: {}\n- CODEX_HOME: `{}`\n- Entries: {}\n\n## Categories\n\n",
        safe(&snapshot["label"]),
        safe(&snapshot["created_at"]),
        safe(&snapshot["codex_home"]),
        rows.len()
    );
    for (name, count) in categories {
        out.push_str(&format!("- `{name}`: {count}\n"));
    }
    out.push_str("\n## Auth/Credential/State Candidates\n\n");
    for e in rows.values().filter(|e| {
        e["sensitive_hint"] == true
            || [
                "config",
                "desktop_state",
                "apps_cache",
                "browser",
                "computer_use",
                "process",
                "sqlite",
                "state",
                "log",
            ]
            .contains(&text(&e["category"]))
    }) {
        out.push_str(&describe(e));
    }
    out.push_str("\n## Most Recently Touched\n\n");
    let mut recent: Vec<_> = rows.values().collect();
    let touched = |e: &Value| {
        e["mtime_ns"]
            .as_i64()
            .unwrap_or(0)
            .max(e["ctime_ns"].as_i64().unwrap_or(0))
    };
    recent.sort_by_key(|e| std::cmp::Reverse(touched(e)));
    for e in recent.into_iter().take(40) {
        out.push_str(&describe(e));
    }
    Ok(out)
}
fn describe(e: &Value) -> String {
    format!(
        "- `{}` [{}] size={} mtime={} ctime={}\n",
        safe(&e["path"]),
        safe(&e["category"]),
        e["size"],
        safe(&e["mtime"]),
        safe(&e["ctime"])
    )
}

pub fn diff_summary(report: &Value, old: &Path, new: &Path) -> String {
    let mut out = format!(
        "# Codex State Diff\n\n- Old: `{}`\n- New: `{}`\n",
        old.display(),
        new.display()
    );
    for kind in ["added", "removed", "changed"] {
        let rows = report[kind].as_array().map(Vec::as_slice).unwrap_or(&[]);
        out.push_str(&format!("\n## {kind}: {}\n\n", rows.len()));
        let limit = if kind == "changed" { 300 } else { 200 };
        for e in rows.iter().take(limit) {
            if kind == "changed" {
                let fields: Vec<_> = e["fields"]
                    .as_object()
                    .map(|v| v.keys().cloned().collect())
                    .unwrap_or_default();
                out.push_str(&format!(
                    "- `{}` [{}] fields={}\n",
                    safe(&e["path"]),
                    safe(&e["new"]["category"]),
                    fields.join(", ")
                ));
            } else {
                out.push_str(&describe(e));
            }
        }
        if rows.len() > limit {
            out.push_str(&format!("- ... {} more\n", rows.len() - limit));
        }
    }
    if report["compatibility_note"] != "" {
        out.push_str(&format!("\n{}\n", safe(&report["compatibility_note"])));
    }
    out
}
