mod control;
mod desktop;
mod receipt;
mod rpc;

use control::{Backend, Local, Submission};
use desktop::Desktop;
use receipt::{Store, uuid, valid_uuid};
use serde_json::json;
use std::io::Read;
use std::path::{Path, PathBuf};
use std::sync::{Arc, atomic::AtomicBool};
use std::time::Duration;

const HELP: &str = "codexm gui — experimental GUI-owned chat control (macOS)\n\n\
codexm gui resume THREAD [--account NAME] [--plan|--terminal]\n\
codexm gui new [--account NAME] [--cwd ABSOLUTE_PATH] [--terminal]\n\
codexm gui status [REQUEST_ID]                 App info / reconcile a saved request\n\
codexm gui list [--limit N] [--cursor CURSOR] [--archived]\n\
codexm gui open THREAD                        Start/open GUI and verify its writer\n\
codexm gui send THREAD --message-stdin [--wait SECONDS] [--request-id UUID]\n\
codexm gui draft [--cwd ABSOLUTE_PATH] [--message-stdin]\n\n\
resume prioritizes an existing GUI owner unless --account explicitly requests CLI.\n\
new starts the official CLI with the selected login (default if omitted).\n\
CLI routes reuse codexm's named tmux windows and independent terminal viewers.\n\
An explicit account never changes the GUI login: release that chat in its GUI first.\n\
--app ABSOLUTE_APP_PATH selects a verified official app bundle.\n\
send reads UTF-8 text from stdin and queues it once for the verified GUI owner.\n\
An active turn finishes before queued input runs; this is not immediate steering.\n\
draft opens a GUI draft; the user must press Send. It does not create a hidden chat.\n\
No account/model overrides, GUI termination, archive/restore, daemon changes or lock edits.\n\
The GUI uses its existing login. A foreign CLI writer is rejected.\n\
Receipts contain IDs/hashes/status, never prompt text. Reusing a request ID never resends.\n\
Native queue entries survive disconnect; a later owner may consume them.\n\
This branch-only command is excluded from normal builds and Homebrew packages.\n";

#[derive(Default, Debug)]
struct Options {
    action: String,
    id: Option<String>,
    app: Option<PathBuf>,
    cwd: Option<PathBuf>,
    message_stdin: bool,
    request_id: Option<String>,
    wait: u64,
    limit: u64,
    cursor: Option<String>,
    archived: bool,
}

fn parse(args: Vec<String>) -> Result<Options, String> {
    let mut args = args.into_iter();
    let mut o = Options {
        action: args.next().unwrap_or("help".into()),
        wait: 10,
        limit: 30,
        ..Options::default()
    };
    if ["-h", "--help"].contains(&o.action.as_str()) {
        o.action = "help".into();
    }
    if !["help", "list", "status", "open", "draft", "send"].contains(&o.action.as_str()) {
        return Err("unknown gui command; use codexm gui help".into());
    }
    let mut seen = std::collections::HashSet::new();
    while let Some(arg) = args.next() {
        if arg.starts_with('-') && !seen.insert(arg.clone()) {
            return Err(format!("duplicate option {arg}"));
        }
        match arg.as_str() {
            "--app" => o.app = Some(PathBuf::from(args.next().ok_or("missing --app path")?)),
            "--cwd" if o.action == "draft" => {
                o.cwd = Some(PathBuf::from(args.next().ok_or("missing --cwd path")?))
            }
            "--message-stdin" if o.action == "draft" || o.action == "send" => {
                o.message_stdin = true
            }
            "--request-id" if o.action == "send" => {
                o.request_id = Some(args.next().ok_or("missing request ID")?)
            }
            "--wait" if o.action == "send" => {
                o.wait = args
                    .next()
                    .ok_or("missing wait seconds")?
                    .parse()
                    .map_err(|_| "invalid wait seconds")?
            }
            "--limit" if o.action == "list" => {
                o.limit = args
                    .next()
                    .ok_or("missing limit")?
                    .parse()
                    .map_err(|_| "invalid limit")?
            }
            "--cursor" if o.action == "list" => {
                o.cursor = Some(args.next().ok_or("missing cursor")?)
            }
            "--archived" if o.action == "list" => o.archived = true,
            _ if !arg.starts_with('-')
                && ["open", "send", "status"].contains(&o.action.as_str())
                && o.id.is_none() =>
            {
                o.id = Some(arg)
            }
            _ => return Err(format!("unsupported gui option: {arg}")),
        }
    }
    if o.wait > 60 || o.limit == 0 || o.limit > 100 {
        return Err("wait must be 0–60 seconds; limit must be 1–100".into());
    }
    if ["send", "open"].contains(&o.action.as_str()) && o.id.is_none() {
        return Err("a full chat UUID is required".into());
    }
    if o.action == "send" && !o.message_stdin {
        return Err(
            "send requires --message-stdin (text is not passed in process arguments)".into(),
        );
    }
    if o.id.as_ref().is_some_and(|x| !valid_uuid(x))
        || o.request_id.as_ref().is_some_and(|x| !valid_uuid(x))
    {
        return Err("IDs must be lowercase UUIDs".into());
    }
    if o.app.as_ref().is_some_and(|p| !p.is_absolute()) {
        return Err("--app must be absolute".into());
    }
    Ok(o)
}

fn read_message() -> Result<String, String> {
    let mut bytes = Vec::new();
    std::io::stdin()
        .take(65537)
        .read_to_end(&mut bytes)
        .map_err(|e| e.to_string())?;
    if bytes.len() > 65536 {
        return Err("message exceeds 65536 bytes".into());
    }
    let text = String::from_utf8(bytes).map_err(|_| "message must be UTF-8")?;
    if text.trim().is_empty() || text.contains('\0') {
        return Err("message is empty or contains NUL".into());
    }
    Ok(text)
}

fn encode_component(value: &str) -> String {
    value
        .bytes()
        .map(|b| {
            if b.is_ascii_alphanumeric() || b"-._~".contains(&b) {
                (b as char).to_string()
            } else {
                format!("%{b:02X}")
            }
        })
        .collect()
}

fn draft_url(cwd: Option<&Path>, message: Option<&str>) -> Result<String, String> {
    let mut params = Vec::new();
    if let Some(path) = cwd {
        if !path.is_absolute() || !path.is_dir() {
            return Err("--cwd must be an existing absolute directory".into());
        }
        let path = path.canonicalize().map_err(|e| e.to_string())?;
        params.push(format!(
            "path={}",
            encode_component(path.to_str().ok_or("cwd must be UTF-8")?)
        ));
    }
    if let Some(text) = message {
        params.push(format!("prompt={}", encode_component(text)));
    }
    let url = if params.is_empty() {
        "codex://threads/new".into()
    } else {
        format!("codex://threads/new?{}", params.join("&"))
    };
    if url.len() > 16000 {
        return Err(
            "draft URL is too long; open a new chat and paste the message in the GUI".into(),
        );
    }
    Ok(url)
}

fn print(value: &impl serde::Serialize) -> Result<(), String> {
    println!(
        "{}",
        serde_json::to_string(value).map_err(|e| e.to_string())?
    );
    Ok(())
}

fn store(home: &Path) -> Result<Store, String> {
    let base = std::env::var_os("XDG_STATE_HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|| home.join(".local/state"));
    Store::open(base.join("codexm/gui-control/requests"))
}

fn execute(o: Options) -> Result<i32, String> {
    if o.action == "help" {
        print!("{HELP}");
        return Ok(0);
    }
    let cancelled = Arc::new(AtomicBool::new(false));
    for signal in [
        signal_hook::consts::SIGINT,
        signal_hook::consts::SIGTERM,
        signal_hook::consts::SIGHUP,
    ] {
        signal_hook::flag::register(signal, cancelled.clone()).map_err(|e| e.to_string())?;
    }
    // Consume/validate text before opening or starting any GUI/native process.
    let message = if o.message_stdin {
        Some(read_message()?)
    } else {
        None
    };
    let desktop = Desktop::discover(o.app, cancelled.clone())?;
    if o.action == "draft" {
        let url = draft_url(o.cwd.as_deref(), message.as_deref())?;
        desktop.open_url(&url)?;
        print(
            &json!({"state":"draft_open_requested","sent":false,"detail":"GUI launch/navigation requested. Review the draft and press Send in the GUI."}),
        )?;
        return Ok(0);
    }
    if o.action == "status" && o.id.is_none() {
        print(
            &json!({"experiment":true,"app":desktop.app,"runtime":desktop.runtime_version()?,"native_home":desktop.native_home,
            "account":"GUI's current login (no CLI account override)","transport":"official deep links + short-lived official queue client","installed_mode_changed":false}),
        )?;
        return Ok(0);
    }
    if o.action == "status" {
        let r = store(&desktop.home)?
            .read(o.id.as_deref().unwrap())?
            .ok_or("request receipt not found")?;
        let observed = match desktop.rpc() {
            Ok(rpc) => control::observe(
                &mut Local {
                    desktop: &desktop,
                    rpc,
                },
                &r,
            ),
            Err(_) => {
                let mut r = r;
                r.detail =
                    "Live observation unavailable; showing saved receipt. Do not resend.".into();
                r
            }
        };
        print(&observed)?;
        return Ok(0);
    }
    let mut backend = Local {
        rpc: desktop.rpc()?,
        desktop: &desktop,
    };
    match o.action.as_str() {
        "list" => {
            let page=backend.read("thread/list",json!({"limit":o.limit,"archived":o.archived,"useStateDbOnly":true,"sortKey":"updated_at","cursor":o.cursor}))?;
            let rows:Vec<_>=page["data"].as_array().ok_or("invalid chat catalog")?.iter().map(|t|json!({"id":t["id"],"title":t["name"],"cwd":t["cwd"],"model":t["model"],"effort":t["reasoningEffort"],"updated_at":t["updatedAt"]})).collect();
            print(
                &json!({"chats":rows,"next_cursor":page["nextCursor"],"archived":o.archived,"source":"native stored catalog; not live GUI status"}),
            )?;
        }
        "open" => {
            let id = o.id.unwrap();
            control::active_chat(&mut backend, &id)?;
            let owner = backend.open(&id)?;
            print(
                &json!({"thread_id":id,"state":"gui_writer_verified","owner":owner,"sent":false}),
            )?;
        }
        "send" => {
            let store = store(&desktop.home)?;
            let id = o.id.unwrap();
            let request = o.request_id.unwrap_or(uuid()?);
            let receipt = control::submit(
                &mut backend,
                &store,
                Submission {
                    thread: &id,
                    request: &request,
                    text: message.as_deref().unwrap(),
                    wait: Duration::from_secs(o.wait),
                },
                &cancelled,
            )?;
            let ok = ["accepted", "started", "completed"].contains(&receipt.state.as_str());
            print(&receipt)?;
            return Ok(if ok { 0 } else { 1 });
        }
        _ => unreachable!(),
    }
    Ok(0)
}

fn main() {
    let code = match parse(std::env::args().skip(1).collect()).and_then(execute) {
        Ok(code) => code,
        Err(e) => {
            eprintln!("codexm gui: {e}");
            1
        }
    };
    std::process::exit(code);
}

#[cfg(test)]
mod tests {
    use super::*;
    fn args(v: &[&str]) -> Vec<String> {
        v.iter().map(|s| (*s).into()).collect()
    }
    #[test]
    fn rejects_unsupported_account_takeover_and_mutation_options() {
        for v in [
            vec!["send", "--account", "default"],
            vec!["draft", "--send"],
            vec!["open", "--force"],
            vec!["list", "--limit", "0"],
            vec!["send", "--wait", "999"],
        ] {
            assert!(parse(args(&v)).is_err());
        }
        assert!(
            parse(args(&[
                "send",
                "00000000-0000-4000-8000-000000000001",
                "--message-stdin",
                "--wait",
                "0"
            ]))
            .is_ok()
        );
    }
    #[test]
    fn deep_link_is_literal_encoded_draft_only() {
        let url = draft_url(None, Some("한글 & / ? $(touch x)\nnext")).unwrap();
        assert!(url.starts_with("codex://threads/new?prompt="));
        assert!(!url.contains("& /"));
        assert!(!url.contains("\n"));
        assert!(!url.contains("submit"));
        assert!(url.contains("%26"));
        assert_eq!(draft_url(None, None).unwrap(), "codex://threads/new");
        assert!(draft_url(Some(Path::new("../relative")), None).is_err());
    }
}
