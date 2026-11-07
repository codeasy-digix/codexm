use crate::rpc::{Rpc, clean_command};
use serde::{Deserialize, Serialize};
use std::io::Read;
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};
use std::time::{Duration, Instant};

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Process {
    pub pid: u32,
    pub parent: u32,
    pub started: String,
    pub executable: String,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Owner {
    pub gui: Process,
    pub writer: Process,
}

pub struct Desktop {
    pub app: PathBuf,
    pub home: PathBuf,
    pub native_home: PathBuf,
    pub binary: PathBuf,
    pub cancelled: Arc<AtomicBool>,
}

pub fn run(mut command: Command, cancelled: &AtomicBool) -> Result<(bool, String), String> {
    let mut child = command
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|e| format!("cannot launch local helper: {e}"))?;
    let output = child.stdout.take().ok_or("missing local helper output")?;
    let reader = std::thread::spawn(move || {
        let mut bytes = Vec::new();
        let _ = output.take(1024 * 1024 + 1).read_to_end(&mut bytes);
        bytes
    });
    let deadline = Instant::now() + Duration::from_secs(10);
    loop {
        if let Some(status) = child.try_wait().map_err(|e| e.to_string())? {
            let bytes = reader.join().map_err(|_| "local helper output failed")?;
            if bytes.len() > 1024 * 1024 {
                return Err("local helper output exceeded limit".into());
            }
            return Ok((
                status.success(),
                String::from_utf8_lossy(&bytes).into_owned(),
            ));
        }
        if cancelled.load(Ordering::Relaxed) || Instant::now() >= deadline {
            let _ = nix::sys::signal::kill(
                nix::unistd::Pid::from_raw(child.id() as i32),
                nix::sys::signal::Signal::SIGTERM,
            );
            // The child is a bounded OS utility, never the GUI or its writer.
            let end = Instant::now() + Duration::from_secs(2);
            while Instant::now() < end {
                if matches!(child.try_wait(), Ok(Some(_))) {
                    break;
                }
                std::thread::sleep(Duration::from_millis(20));
            }
            return Err("local helper timed out or was cancelled".into());
        }
        std::thread::sleep(Duration::from_millis(20));
    }
}

impl Desktop {
    pub fn discover(app: Option<PathBuf>, cancelled: Arc<AtomicBool>) -> Result<Self, String> {
        if !cfg!(target_os = "macos") {
            return Err("GUI control experiment currently supports macOS only; existing CLI/TUI is unchanged".into());
        }
        let home = PathBuf::from(std::env::var_os("HOME").ok_or("HOME is unavailable")?)
            .canonicalize()
            .map_err(|e| e.to_string())?;
        let app = app
            .or_else(|| {
                [
                    PathBuf::from("/Applications/ChatGPT.app"),
                    PathBuf::from("/Applications/Codex.app"),
                    home.join("Applications/ChatGPT.app"),
                    home.join("Applications/Codex.app"),
                ]
                .into_iter()
                .find(|p| p.is_dir())
            })
            .ok_or("install the official ChatGPT/Codex desktop app first")?
            .canonicalize()
            .map_err(|e| e.to_string())?;
        if !app.is_absolute() || app.extension().and_then(|x| x.to_str()) != Some("app") {
            return Err("--app must identify an installed .app bundle".into());
        }
        let mut verify = Command::new("/usr/bin/codesign");
        verify.args(["--verify", "-R", "=anchor apple generic and identifier \"com.openai.codex\" and certificate leaf[subject.OU] = \"2DC432GLL2\""]).arg(&app);
        if !run(verify, &cancelled)?.0 {
            return Err("app is not the verified OpenAI Codex desktop bundle".into());
        }
        let candidates = [
            "Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex",
            "Contents/Resources/codex",
        ];
        let binary = candidates
            .into_iter()
            .map(|p| app.join(p))
            .find(|p| p.is_file())
            .ok_or("verified app has no supported bundled Codex runtime")?
            .canonicalize()
            .map_err(|e| e.to_string())?;
        if !binary.starts_with(&app) {
            return Err("bundled runtime resolves outside the verified app".into());
        }
        let native_home = home
            .join(".codex")
            .canonicalize()
            .map_err(|_| "native desktop home does not exist; sign in using the GUI first")?;
        Ok(Self {
            app,
            home,
            native_home,
            binary,
            cancelled,
        })
    }

    pub fn runtime_version(&self) -> Result<String, String> {
        let mut command = clean_command(&self.binary, &self.home, &self.native_home);
        command.arg("--version");
        let (ok, version) = run(command, &self.cancelled)?;
        if !ok {
            return Err("cannot identify bundled runtime".into());
        }
        Ok(version.trim().into())
    }

    pub fn rpc(&self) -> Result<Rpc, String> {
        let version = self.runtime_version()?;
        if !["codex-cli 0.167.0", "codex-cli 0.167.0-alpha.3.2"].contains(&version.as_str()) {
            return Err(format!(
                "runtime {version} is not validated for this experiment; no request submitted"
            ));
        }
        Rpc::start(
            &self.binary,
            &self.home,
            &self.native_home,
            self.cancelled.clone(),
        )
    }

    pub fn open_url(&self, url: &str) -> Result<(), String> {
        let mut command = clean_command(Path::new("/usr/bin/open"), &self.home, &self.native_home);
        command.arg("-a").arg(&self.app).arg(url);
        if !run(command, &self.cancelled)?.0 {
            return Err("LaunchServices did not accept the GUI open request".into());
        }
        Ok(())
    }

    pub fn process(&self, pid: u32) -> Result<Process, String> {
        let mut command = Command::new("/bin/ps");
        command
            .env("LC_ALL", "C")
            .args(["-p", &pid.to_string(), "-o", "uid=,ppid=,lstart=,comm="]);
        let (ok, text) = run(command, &self.cancelled)?;
        let f: Vec<_> = text.split_whitespace().collect();
        if !ok || f.len() < 8 || f[0] != nix::unistd::Uid::current().to_string() {
            return Err("writer process disappeared or is not owned by this user".into());
        }
        Ok(Process {
            pid,
            parent: f[1].parse().map_err(|_| "invalid parent PID")?,
            started: f[2..7].join(" "),
            executable: f[7..].join(" "),
        })
    }

    /// No creation, reads, flock, or deletion of the native lock file.
    pub fn owner(&self, thread: &str) -> Result<Option<Owner>, String> {
        let file = self
            .native_home
            .join("thread-writer-locks")
            .join(format!("{thread}.lock"));
        match std::fs::symlink_metadata(&file) {
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(e) => return Err(format!("cannot inspect native writer: {e}")),
            Ok(m) if !m.is_file() || m.uid() != nix::unistd::Uid::current().as_raw() => {
                return Err("native writer file has unexpected ownership/type".into());
            }
            Ok(_) => {}
        }
        let mut command = Command::new("/usr/sbin/lsof");
        command.args(["-nP", "-t", "--"]).arg(file);
        let (ok, text) = run(command, &self.cancelled)?;
        let mut ids: Vec<u32> = text
            .split_whitespace()
            .map(str::parse)
            .collect::<Result<_, _>>()
            .map_err(|_| "invalid writer lookup")?;
        ids.sort_unstable();
        ids.dedup();
        if ids.is_empty() {
            return Ok(None);
        }
        if !ok || ids.len() != 1 || ids[0] <= 1 {
            return Err("cannot verify exactly one native writer; no request submitted".into());
        }
        let writer = self.process(ids[0])?;
        if Path::new(&writer.executable) != self.binary {
            return Err(
                "another CLI/server owns this chat; it was not stopped and no input was sent"
                    .into(),
            );
        }
        let mut current = writer.clone();
        for _ in 0..12 {
            if current.parent <= 1 {
                break;
            }
            current = self.process(current.parent)?;
            let executable = Path::new(&current.executable);
            if executable.parent() == Some(self.app.join("Contents/MacOS").as_path()) {
                return Ok(Some(Owner {
                    gui: current,
                    writer,
                }));
            }
        }
        Err("writer is not owned by the verified GUI; no input was sent".into())
    }

    pub fn open_thread(&self, thread: &str) -> Result<Owner, String> {
        // Reject a foreign writer before switching the visible GUI chat.
        let original = self.owner(thread)?;
        self.open_url(&format!("codex://threads/{thread}"))?;
        let deadline = Instant::now() + Duration::from_secs(12);
        loop {
            if self.cancelled.load(Ordering::Relaxed) {
                return Err("cancelled before queue submission".into());
            }
            if let Some(owner) = self.owner(thread)? {
                if original.as_ref().is_some_and(|p| p != &owner) {
                    return Err("GUI owner changed while opening chat; no input was sent".into());
                }
                return Ok(owner);
            }
            if Instant::now() >= deadline {
                return Err("GUI open was requested but no GUI writer appeared; check the chat in the app (no input sent)".into());
            }
            std::thread::sleep(Duration::from_millis(150));
        }
    }
}
