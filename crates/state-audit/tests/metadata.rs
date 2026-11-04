use codexm_state_audit as audit;
use serde_json::json;
use std::fs;
use std::os::unix::fs::{DirBuilderExt, PermissionsExt, symlink};
use std::os::unix::net::UnixListener;
use std::path::PathBuf;
use std::process::Command;
use std::sync::atomic::{AtomicU64, Ordering};

static NEXT_FIXTURE: AtomicU64 = AtomicU64::new(0);

struct Fixture(PathBuf);
impl Fixture {
    fn new() -> Self {
        // Clock precision is not a uniqueness guarantee for parallel tests.
        // Atomic creation also preserves stale directories from an older PID.
        loop {
            let dir = std::env::temp_dir().join(format!(
                "codexm-audit-{}-{}",
                std::process::id(),
                NEXT_FIXTURE.fetch_add(1, Ordering::Relaxed)
            ));
            match fs::DirBuilder::new().mode(0o700).create(&dir) {
                Ok(()) => return Self(dir),
                Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => continue,
                Err(error) => panic!("create audit test fixture: {error}"),
            }
        }
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

#[test]
fn metadata_never_reads_secrets_or_descends_symlinks() {
    let f = Fixture::new();
    let home = f.0.join("한글 home");
    let output = f.0.join("out");
    fs::create_dir(&home).unwrap();
    fs::write(home.join("auth.json"), "AUTH-CONTENT-MUST-NOT-APPEAR").unwrap();
    fs::set_permissions(home.join("auth.json"), fs::Permissions::from_mode(0o000)).unwrap();
    fs::create_dir(home.join("sessions")).unwrap();
    fs::write(home.join("sessions/chat.jsonl"), "PRIVATE-CHAT-CONTENT").unwrap();
    let external = f.0.join("external");
    fs::create_dir(&external).unwrap();
    fs::write(external.join("secret"), "private").unwrap();
    symlink(&external, home.join("external-link")).unwrap();
    symlink("missing", home.join("broken-link")).unwrap();
    symlink(&home, home.join("cycle")).unwrap();
    fs::create_dir(home.join("auth-switch-audit")).unwrap();
    fs::write(home.join("auth-switch-audit/old.snapshot.json"), "ignored").unwrap();
    // Socket pathname length is limited on macOS, so use the short temporary root.
    let socket_path = f.0.join("socket");
    let _socket = UnixListener::bind(&socket_path).unwrap();
    symlink(&socket_path, home.join("socket-link")).unwrap();
    let value = audit::snapshot(&home, &output, "../unsafe label").unwrap();
    let encoded = serde_json::to_string(&value).unwrap();
    assert!(!encoded.contains("AUTH-CONTENT"));
    assert!(!encoded.contains("PRIVATE-CHAT"));
    assert!(!encoded.contains("external-link/secret"));
    assert!(!encoded.contains("old.snapshot.json"));
    let entries = value["entries"].as_array().unwrap();
    assert_eq!(
        entries
            .iter()
            .find(|e| e["path"] == "sessions/chat.jsonl")
            .unwrap()["category"],
        "session"
    );
    assert_eq!(
        entries.iter().find(|e| e["path"] == "auth.json").unwrap()["mode"],
        "0o0"
    );
    assert_eq!(
        entries.iter().find(|e| e["path"] == "broken-link").unwrap()["kind"],
        "symlink"
    );
    assert_eq!(
        audit::diff(&value, &audit::snapshot(&home, &output, "after").unwrap()).unwrap()["changed"],
        json!([])
    );
}

#[test]
fn detects_metadata_changes_and_accepts_legacy_without_birthtime_noise() {
    let f = Fixture::new();
    let home = f.0.join("home");
    fs::create_dir(&home).unwrap();
    fs::write(home.join("auth.json"), "old").unwrap();
    fs::write(home.join("remove"), "old").unwrap();
    let before = audit::snapshot(&home, &f.0.join("out"), "before").unwrap();
    fs::write(home.join("auth.json"), "new longer").unwrap();
    fs::remove_file(home.join("remove")).unwrap();
    fs::write(home.join("add"), "new").unwrap();
    let after = audit::snapshot(&home, &f.0.join("out"), "after").unwrap();
    let report = audit::diff(&before, &after).unwrap();
    assert_eq!(report["added"][0]["path"], "add");
    assert_eq!(report["removed"][0]["path"], "remove");
    assert_eq!(report["changed"][0]["path"], "auth.json");
    let mut legacy = after.clone();
    legacy["schema"] = json!(1);
    legacy["entries"][0]["birthtime_ns"] = json!(123);
    assert_eq!(audit::diff(&legacy, &after).unwrap()["changed"], json!([]));
    let mut wrong = after.clone();
    wrong["codex_home"] = json!("/another/home");
    assert!(audit::diff(&before, &wrong).is_err());
    let mut duplicate = after.clone();
    duplicate["entries"]
        .as_array_mut()
        .unwrap()
        .push(after["entries"][0].clone());
    assert!(audit::diff(&before, &duplicate).is_err());
}

#[test]
fn cli_uses_private_xdg_output_and_preserves_native_home() {
    let f = Fixture::new();
    let home = f.0.join("native");
    let state = f.0.join("state");
    fs::create_dir(&home).unwrap();
    fs::write(home.join("config.toml"), "model='fixture'").unwrap();
    let run = |args: &[&str]| {
        Command::new(env!("CARGO_BIN_EXE_codexm-state-audit"))
            .args(args)
            .env("CODEX_HOME", &home)
            .env("XDG_STATE_HOME", &state)
            .output()
            .unwrap()
    };
    assert!(run(&["snapshot", "before"]).status.success());
    assert!(run(&["snapshot", "after"]).status.success());
    assert_eq!(fs::read_dir(&home).unwrap().count(), 1);
    let listed = run(&["list"]);
    assert!(listed.status.success());
    let lines = String::from_utf8(listed.stdout).unwrap();
    assert_eq!(lines.lines().count(), 2);
    for line in lines.lines() {
        assert_eq!(
            fs::metadata(line).unwrap().permissions().mode() & 0o777,
            0o600
        );
    }
    let report = run(&["diff"]);
    assert!(report.status.success());
    assert!(
        String::from_utf8(report.stdout)
            .unwrap()
            .contains("added=0 removed=0 changed=0")
    );
    assert!(!run(&["diff", "only-one"]).status.success());
    let output = audit::default_output(&home.canonicalize().unwrap(), &state);
    assert_eq!(
        fs::metadata(&output).unwrap().permissions().mode() & 0o777,
        0o700
    );
    let link = f.0.join("output-link");
    symlink(&output, &link).unwrap();
    assert!(
        !run(&["--output", link.to_str().unwrap(), "snapshot"])
            .status
            .success()
    );
    assert!(
        !run(&["--output", home.to_str().unwrap(), "snapshot"])
            .status
            .success()
    );
}

#[test]
fn labels_and_categories_cover_native_root_paths() {
    assert_eq!(audit::sanitize_label("../"), "snapshot");
    assert_eq!(audit::sanitize_label("x/../../name"), "x-..-..-name");
    assert_eq!(
        audit::classify("archived_sessions/item.jsonl", "file"),
        "session"
    );
    assert_eq!(
        audit::classify(".credentials.json.bak", "file"),
        "mcp_oauth"
    );
    assert_eq!(audit::classify("state_5.sqlite-wal", "file"), "sqlite");
}
