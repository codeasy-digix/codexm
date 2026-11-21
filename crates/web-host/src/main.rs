use std::env;
use std::error::Error;
use std::ffi::OsString;
use std::fs;
use std::os::unix::process::ExitStatusExt;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitStatus};

use nix::sys::signal::{Signal, kill};
use nix::unistd::Pid;
use signal_hook::consts::{SIGCHLD, SIGINT, SIGTERM};
use signal_hook::iterator::Signals;

fn main() {
    match run() {
        Ok(status) => std::process::exit(
            status
                .code()
                .unwrap_or_else(|| 128 + status.signal().unwrap_or(0)),
        ),
        Err(error) => {
            eprintln!("codexm-web-host: {error}");
            std::process::exit(1);
        }
    }
}

fn run() -> Result<ExitStatus, Box<dyn Error>> {
    let args: Vec<OsString> = env::args_os().skip(1).collect();
    let command = if args.first().is_some_and(|arg| arg == "--config") {
        2
    } else {
        0
    };
    if !args
        .get(command..command + 2)
        .is_some_and(|args| args[0] == "web" && args[1] == "serve")
    {
        return Err("the macOS web host expects [--config PATH] web serve".into());
    }

    let own_path = env::current_exe()?.canonicalize()?;
    let bundle = own_path.ancestors().nth(3).ok_or("missing app bundle")?;
    if bundle
        .file_name()
        .is_none_or(|name| name != "CodexM Web.app")
    {
        return Err("the web host must be run from CodexM Web.app".into());
    }
    let root = bundle.parent().ok_or("missing installation root")?;
    let binary = release(root)?;
    let mut signals = Signals::new([SIGCHLD, SIGINT, SIGTERM])?;
    let mut child = Command::new(binary).args(args).spawn()?;
    let pid = Pid::from_raw(i32::try_from(child.id())?);

    // Stay alive as the launchd job while Node, Codex and the PTY are started
    // by the installed release. Check exit before signaling an unreaped child.
    for received in signals.forever() {
        if let Some(status) = child.try_wait()? {
            return Ok(status);
        }
        if let Ok(signal @ (Signal::SIGINT | Signal::SIGTERM)) = Signal::try_from(received) {
            let _ = kill(pid, signal);
        }
    }
    Ok(child.wait()?)
}

fn release(root: &Path) -> Result<PathBuf, Box<dyn Error>> {
    let link = root.join("bin/codexm-web-release");
    if !fs::symlink_metadata(&link)?.file_type().is_symlink() {
        return Err("the web release entry must be a symbolic link".into());
    }
    let binary = link.canonicalize()?;
    if !binary.starts_with(root.join("versions"))
        || binary.file_name().is_none_or(|name| name != "codexm")
        || !binary.is_file()
    {
        return Err("the web release is outside the installed CodexM versions".into());
    }
    Ok(binary)
}
