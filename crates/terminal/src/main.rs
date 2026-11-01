mod bridge;
mod input;
mod model;
#[cfg(test)]
mod tests;
mod ui;

use crossterm::event::{self, Event, KeyEventKind};
use model::{Action, App, Preferences, View};
use serde_json::json;
use std::env;
use std::io::{self, IsTerminal};
use std::os::unix::process::CommandExt;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};
use std::time::{Duration, Instant};

fn run() -> io::Result<()> {
    let mut args = env::args().skip(1);
    let mut binary = None;
    let mut config = None;
    while let Some(arg) = args.next() {
        match arg.as_str() {
            "--version" => {
                println!("codexm-tui {}", env!("CARGO_PKG_VERSION"));
                return Ok(());
            }
            "--bridge" => binary = args.next().map(PathBuf::from),
            "--config" => config = args.next().map(PathBuf::from),
            _ => {
                return Err(io::Error::other(
                    "use codexm to open this terminal interface",
                ));
            }
        }
    }
    let binary = binary.ok_or_else(|| io::Error::other("missing installed bridge"))?;
    let config = config.ok_or_else(|| io::Error::other("missing configuration path"))?;
    if !binary.is_absolute() || !config.is_absolute() {
        return Err(io::Error::other(
            "bridge and configuration paths must be absolute",
        ));
    }
    if !io::stdin().is_terminal() || !io::stdout().is_terminal() {
        return Err(io::Error::other(
            "interactive terminal required; use codexm list --json",
        ));
    }
    let mut prefs_name = config.as_os_str().to_os_string();
    prefs_name.push(".tui.json");
    let prefs = PathBuf::from(prefs_name);
    let mut app = App::new(Preferences::load(&prefs)?);
    let terminated = Arc::new(AtomicBool::new(false));
    for signal in [
        signal_hook::consts::SIGTERM,
        signal_hook::consts::SIGHUP,
        signal_hook::consts::SIGINT,
    ] {
        signal_hook::flag::register(signal, terminated.clone())?;
    }
    let mut bridge = bridge::Bridge::start(&binary, &config)?;
    bridge.send("snapshot", json!({}))?;
    app.request_started("snapshot");
    let mut last_refresh = Instant::now();
    let mut last_login = Instant::now();
    let mut resume = None;
    let mut pending: Option<(&'static str, serde_json::Value)> = None;
    let result = ratatui::run(|terminal| -> io::Result<()> {
        loop {
            if terminated.load(Ordering::Relaxed) {
                break;
            }
            while let Ok(reply) = bridge.replies.try_recv() {
                app.reply(&reply.request.method, &reply.request.params, reply.result);
            }
            if app.busy.is_none()
                && let Some((method, params)) = pending.take()
            {
                bridge.send(method, params)?;
                app.request_started(method);
            }
            if app
                .busy
                .is_some_and(|start| start.elapsed() > Duration::from_secs(125))
            {
                return Err(io::Error::other(
                    "response timed out; no request was replayed",
                ));
            }
            terminal.draw(|f| ui::draw(f, &app))?;
            let action = if event::poll(Duration::from_millis(100))? {
                match event::read()? {
                    Event::Key(key) if key.kind != KeyEventKind::Release => app.key(key),
                    _ => Action::None,
                }
            } else {
                Action::None
            };
            match action {
                Action::Quit => break,
                Action::Resume { thread, account } => {
                    resume = Some((thread, account));
                    break;
                }
                Action::Request { method, params } => {
                    if app.busy.is_some() {
                        // Navigation stays responsive during reads. An explicit
                        // action waits once for that read; never replay mutations.
                        pending = Some((method, params));
                        app.blocking = true;
                    } else {
                        bridge.send(method, params)?;
                        app.request_started(method);
                    }
                }
                Action::SavePreferences => match app.preferences.save(&prefs) {
                    Ok(()) => app.notice = "목록 표시 설정을 저장했습니다.".into(),
                    Err(e) => app.notice = format!("오류: {e}"),
                },
                Action::OpenURL(url) => {
                    // The bridge already restricts official HTTPS auth URLs.
                    if !(url.starts_with("https://auth.openai.com/")
                        || url.starts_with("https://auth0.openai.com/")
                        || url.starts_with("https://chatgpt.com/"))
                    {
                        app.notice = "지원하지 않는 로그인 주소입니다.".into();
                    } else {
                        let launcher = if cfg!(target_os = "macos") {
                            "open"
                        } else {
                            "xdg-open"
                        };
                        match Command::new(launcher)
                            .arg(&url)
                            .stdin(Stdio::null())
                            .stdout(Stdio::null())
                            .stderr(Stdio::null())
                            .spawn()
                        {
                            Ok(mut child) => {
                                std::thread::spawn(move || {
                                    let _ = child.wait();
                                });
                            }
                            Err(_) => {
                                app.notice =
                                    "브라우저를 열지 못했습니다. 표시된 주소를 직접 열어주세요."
                                        .into()
                            }
                        }
                    }
                }
                Action::None => {}
            }
            if app.busy.is_none() {
                if app.view == View::Login && last_login.elapsed() > Duration::from_secs(1) {
                    bridge.send("login.status", json!({}))?;
                    app.request_started("login.status");
                    last_login = Instant::now();
                } else if app.refresh_needed
                    || (app.view != View::Login && last_refresh.elapsed() > Duration::from_secs(5))
                {
                    app.refresh_needed = false;
                    bridge.send("snapshot", json!({}))?;
                    app.request_started("snapshot");
                    last_refresh = Instant::now();
                }
            }
        }
        Ok(())
    });
    bridge.stop();
    result?;
    if let Some((thread, account)) = resume {
        // Terminal state and owned auth helpers are gone before handing stdin
        // and the same process to the existing official-CLI launcher.
        return Err(Command::new(&binary)
            .arg("--config")
            .arg(&config)
            .args(["resume", &thread, "--account", &account])
            .exec());
    }
    Ok(())
}
fn main() {
    if let Err(e) = run() {
        eprintln!("codexm: {e}");
        std::process::exit(1);
    }
}
