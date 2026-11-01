use codexm_state_audit as audit;
use serde_json::Value;
use std::env;
use std::fs::{self, File, OpenOptions};
use std::io::{self, Read, Write};
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};
use time::{OffsetDateTime, macros::format_description};

const HELP: &str = "codexm-state-audit [--home PATH] [--output DIRECTORY] snapshot [LABEL]\n\
codexm-state-audit [--home PATH] [--output DIRECTORY] diff [OLD NEW]\n\
codexm-state-audit [--home PATH] [--output DIRECTORY] list\n\n\
Manual filesystem metadata only. Never reads file contents, follows directory\n\
symlinks, or modifies Codex data. Default output: XDG_STATE_HOME/codexm/state-audit.\n\
Existing Python schema-1 snapshots are readable; no Python runtime is required.";

fn expand(value: &str) -> io::Result<PathBuf> {
    let path = if value == "~" || value.starts_with("~/") {
        PathBuf::from(env::var_os("HOME").ok_or_else(|| io::Error::other("HOME is not set"))?)
            .join(value.trim_start_matches('~').trim_start_matches('/'))
    } else {
        PathBuf::from(value)
    };
    if path.is_absolute() {
        Ok(path)
    } else {
        Ok(env::current_dir()?.join(path))
    }
}

fn private_dir(path: &Path) -> io::Result<()> {
    if !path.exists() {
        fs::DirBuilder::new()
            .recursive(true)
            .mode(0o700)
            .create(path)?;
    }
    if !fs::symlink_metadata(path)?.is_dir() {
        return Err(io::Error::other("output must be a real directory"));
    }
    fs::set_permissions(path, fs::Permissions::from_mode(0o700))
}

fn resolve_output(path: &Path) -> io::Result<PathBuf> {
    if fs::symlink_metadata(path).is_ok_and(|m| m.file_type().is_symlink()) {
        return Err(io::Error::other("output must not be a symlink"));
    }
    fn resolve(path: &Path) -> io::Result<PathBuf> {
        if path.exists() {
            return path.canonicalize();
        }
        let parent = path
            .parent()
            .ok_or_else(|| io::Error::other("output has no parent"))?;
        let name = path
            .file_name()
            .ok_or_else(|| io::Error::other("output must have a directory name"))?;
        Ok(resolve(parent)?.join(name))
    }
    resolve(path)
}

fn write_new(path: &Path, data: &[u8]) -> io::Result<()> {
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)?;
    if let Err(e) = file.write_all(data).and_then(|_| file.sync_all()) {
        let _ = fs::remove_file(path);
        return Err(e);
    }
    Ok(())
}
fn write_json(path: &Path, value: &Value) -> io::Result<()> {
    let mut bytes = serde_json::to_vec_pretty(value)?;
    bytes.push(b'\n');
    write_new(path, &bytes)
}
fn load(path: &Path) -> io::Result<Value> {
    let file = File::open(path)?;
    if !file.metadata()?.is_file() || file.metadata()?.len() > 128 * 1024 * 1024 {
        return Err(io::Error::other(
            "snapshot must be a regular JSON file under 128 MiB",
        ));
    }
    serde_json::from_reader(io::BufReader::new(file.take(128 * 1024 * 1024 + 1)))
        .map_err(io::Error::other)
}
fn latest(output: &Path) -> io::Result<Vec<PathBuf>> {
    let mut result = Vec::new();
    if !output.exists() {
        return Ok(result);
    }
    for entry in fs::read_dir(output)? {
        let entry = entry?;
        if entry.file_type()?.is_file()
            && entry
                .file_name()
                .to_str()
                .is_some_and(|n| n.ends_with(".snapshot.json"))
        {
            result.push(entry.path());
        }
    }
    result.sort();
    Ok(result)
}
fn stamp() -> io::Result<String> {
    OffsetDateTime::now_utc()
        .format(format_description!(
            "[year][month][day]T[hour][minute][second][subsecond digits:9]Z"
        ))
        .map_err(io::Error::other)
}

fn run() -> io::Result<()> {
    let mut args = env::args().skip(1);
    let mut home = None;
    let mut output = None;
    let mut positional = Vec::new();
    while let Some(arg) = args.next() {
        match arg.as_str() {
            "--help" | "-h" => {
                println!("{HELP}");
                return Ok(());
            }
            "--version" => {
                println!("codexm-state-audit {}", env!("CARGO_PKG_VERSION"));
                return Ok(());
            }
            "--home" => {
                home = Some(expand(
                    &args
                        .next()
                        .ok_or_else(|| io::Error::other("--home needs a path"))?,
                )?)
            }
            "--output" => {
                output =
                    Some(expand(&args.next().ok_or_else(|| {
                        io::Error::other("--output needs a directory")
                    })?)?)
            }
            value if value.starts_with('-') => {
                return Err(io::Error::other("unknown option; use --help"));
            }
            _ => positional.push(arg),
        }
    }
    let home = home
        .unwrap_or(expand(
            &env::var("CODEX_HOME").unwrap_or_else(|_| "~/.codex".into()),
        )?)
        .canonicalize()?;
    let output = resolve_output(&output.unwrap_or(audit::default_output(
        &home,
        &expand(&env::var("XDG_STATE_HOME").unwrap_or_else(|_| "~/.local/state".into()))?,
    )))?;
    if home.starts_with(&output) {
        return Err(io::Error::other(
            "output cannot be the Codex home or its ancestor",
        ));
    }
    match positional.first().map(String::as_str) {
        Some("snapshot") if positional.len() <= 2 => {
            let label = positional.get(1).map(String::as_str).unwrap_or("snapshot");
            let value = audit::snapshot(&home, &output, label)?;
            let summary = audit::snapshot_summary(&value)?;
            private_dir(&output)?;
            let prefix = format!("{}_{}", stamp()?, audit::sanitize_label(label));
            let json_path = output.join(format!("{prefix}.snapshot.json"));
            let md_path = output.join(format!("{prefix}.summary.md"));
            write_json(&json_path, &value)?;
            write_new(&md_path, summary.as_bytes())?;
            println!(
                "snapshot={}\nsummary={}\nentries={}",
                json_path.display(),
                md_path.display(),
                value["entries"].as_array().map_or(0, Vec::len)
            );
        }
        Some("list") if positional.len() == 1 => {
            for path in latest(&output)? {
                println!("{}", path.display());
            }
        }
        Some("diff") if positional.len() == 1 || positional.len() == 3 => {
            let (old, new) = if positional.len() == 3 {
                (expand(&positional[1])?, expand(&positional[2])?)
            } else {
                let files = latest(&output)?;
                if files.len() < 2 {
                    return Err(io::Error::other(
                        "need at least two snapshots; use --output for an existing audit directory",
                    ));
                }
                (
                    files[files.len() - 2].clone(),
                    files[files.len() - 1].clone(),
                )
            };
            let mut value = audit::diff(&load(&old)?, &load(&new)?)?;
            value["old_snapshot"] = Value::from(old.to_string_lossy().as_ref());
            value["new_snapshot"] = Value::from(new.to_string_lossy().as_ref());
            let summary = audit::diff_summary(&value, &old, &new);
            private_dir(&output)?;
            let prefix = stamp()?;
            let json_path = output.join(format!("{prefix}_diff.json"));
            let md_path = output.join(format!("{prefix}_diff.md"));
            write_json(&json_path, &value)?;
            write_new(&md_path, summary.as_bytes())?;
            println!(
                "diff_json={}\ndiff_summary={}\nadded={} removed={} changed={}",
                json_path.display(),
                md_path.display(),
                value["added"].as_array().map_or(0, Vec::len),
                value["removed"].as_array().map_or(0, Vec::len),
                value["changed"].as_array().map_or(0, Vec::len)
            );
        }
        _ => return Err(io::Error::other(HELP)),
    }
    Ok(())
}
fn main() {
    if let Err(e) = run() {
        eprintln!("codexm-state-audit: {e}");
        std::process::exit(1);
    }
}
