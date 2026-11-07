//! A bounded, short-lived official app-server. This connection never loads a chat.
use serde_json::{Value, json};
use std::io::{BufRead, BufReader, Read, Write};
use std::path::Path;
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
    mpsc,
};
use std::time::{Duration, Instant};

const MAX_FRAME: u64 = 32 * 1024 * 1024;
pub const READ_METHODS: &[&str] = &[
    "thread/read",
    "thread/list",
    "thread/turns/list",
    "thread/queue/list",
];

#[derive(Debug)]
pub enum Failure {
    Rejected(String),
    Uncertain(String),
}

impl std::fmt::Display for Failure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Rejected(s) | Self::Uncertain(s) => f.write_str(s),
        }
    }
}

pub struct Rpc {
    child: Child,
    input: Option<ChildStdin>,
    responses: mpsc::Receiver<Result<Value, String>>,
    next: u64,
    cancelled: Arc<AtomicBool>,
}

pub fn clean_command(binary: &Path, home: &Path, native_home: &Path) -> Command {
    let mut c = Command::new(binary);
    c.env_clear()
        .env("HOME", home)
        .env("CODEX_HOME", native_home)
        .env("CODEX_SQLITE_HOME", native_home)
        .env("LANG", "en_US.UTF-8")
        .env("PATH", "/usr/bin:/bin:/usr/sbin:/sbin");
    if let Some(tmp) = std::env::var_os("TMPDIR") {
        c.env("TMPDIR", tmp);
    }
    c
}

impl Rpc {
    pub fn start(
        binary: &Path,
        home: &Path,
        native_home: &Path,
        cancelled: Arc<AtomicBool>,
    ) -> Result<Self, String> {
        let mut cmd = clean_command(binary, home, native_home);
        cmd.args([
            "--no-daemon",
            "--disable",
            "plugins",
            "--disable",
            "apps",
            "--disable",
            "hooks",
            "--enable",
            "skip_host_skill_discovery",
            "app-server",
            "--listen",
            "stdio://",
        ])
        .current_dir(home)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
        let mut child = cmd
            .spawn()
            .map_err(|e| format!("cannot start temporary official queue client: {e}"))?;
        let input = child.stdin.take();
        let stdout = child.stdout.take().ok_or("missing RPC stdout")?;
        let (sender, responses) = mpsc::sync_channel(32);
        std::thread::spawn(move || {
            let mut reader = BufReader::new(stdout);
            loop {
                let mut b = Vec::new();
                let read = std::io::Read::by_ref(&mut reader)
                    .take(MAX_FRAME + 1)
                    .read_until(b'\n', &mut b);
                let result = match read {
                    Ok(0) => Err("official queue client closed".into()),
                    Ok(_) if b.len() as u64 > MAX_FRAME => Err("RPC frame exceeds limit".into()),
                    Ok(_) => serde_json::from_slice(&b).map_err(|_| "invalid RPC response".into()),
                    Err(_) => Err("cannot read RPC response".into()),
                };
                let terminal = result.is_err();
                if sender.send(result).is_err() || terminal {
                    break;
                }
            }
        });
        let mut rpc = Self {
            child,
            input,
            responses,
            next: 0,
            cancelled,
        };
        rpc.call("initialize", json!({"clientInfo":{"name":"codexm_gui_experiment","version":env!("CARGO_PKG_VERSION")},"capabilities":{"experimentalApi":true}}))
            .map_err(|e| e.to_string())?;
        rpc.write(&json!({"method":"initialized","params":{}}))
            .map_err(|e| e.to_string())?;
        Ok(rpc)
    }

    fn write(&mut self, value: &Value) -> Result<(), Failure> {
        let bytes =
            serde_json::to_vec(value).map_err(|_| Failure::Rejected("cannot encode RPC".into()))?;
        let input = self
            .input
            .as_mut()
            .ok_or_else(|| Failure::Uncertain("RPC disconnected".into()))?;
        input
            .write_all(&bytes)
            .and_then(|()| input.write_all(b"\n"))
            .and_then(|()| input.flush())
            .map_err(|_| Failure::Uncertain("RPC transport lost; do not resend".into()))
    }

    fn call(&mut self, method: &str, params: Value) -> Result<Value, Failure> {
        if self.cancelled.load(Ordering::Relaxed) {
            return Err(Failure::Rejected("cancelled before submission".into()));
        }
        self.next += 1;
        let id = self.next;
        self.write(&json!({"id":id,"method":method,"params":params}))?;
        let deadline = Instant::now() + Duration::from_secs(10);
        loop {
            if self.cancelled.load(Ordering::Relaxed) || Instant::now() >= deadline {
                return Err(Failure::Uncertain(format!(
                    "{method} response unobserved; do not resend"
                )));
            }
            match self.responses.recv_timeout(Duration::from_millis(50)) {
                Ok(Ok(r)) if r["id"].as_u64() == Some(id) && r.get("method").is_none() => {
                    if let Some(error) = r.get("error") {
                        let message = format!(
                            "official app-server rejected {method} (code {})",
                            error["code"]
                        );
                        return Err(
                            if [-32600, -32601, -32602]
                                .contains(&error["code"].as_i64().unwrap_or(0))
                            {
                                Failure::Rejected(message)
                            } else {
                                Failure::Uncertain(format!(
                                    "{message}; mutation outcome unknown; do not resend"
                                ))
                            },
                        );
                    }
                    return r.get("result").cloned().ok_or_else(|| {
                        Failure::Uncertain("RPC response has no result; do not resend".into())
                    });
                }
                Ok(Ok(r)) if r.get("method").is_some() && r.get("id").is_some() => {
                    // A relay must never authorize tools, approvals, or model work.
                    self.write(&json!({"id":r["id"],"error":{"code":-32601,"message":"This read/queue client cannot handle execution requests"}}))?;
                }
                Ok(Ok(_)) => {}
                Ok(Err(e)) => return Err(Failure::Uncertain(e)),
                Err(mpsc::RecvTimeoutError::Timeout) => {}
                Err(_) => {
                    return Err(Failure::Uncertain(
                        "RPC transport closed; do not resend".into(),
                    ));
                }
            }
        }
    }

    pub fn read(&mut self, method: &str, params: Value) -> Result<Value, String> {
        if !READ_METHODS.contains(&method) {
            return Err("method is not a permitted read".into());
        }
        self.call(method, params).map_err(|e| e.to_string())
    }

    pub fn enqueue(&mut self, thread: &str, request: &str, text: &str) -> Result<Value, Failure> {
        self.call(
            "thread/queue/add",
            json!({"threadId":thread,"clientUserMessageId":request,
            "input":[{"type":"text","text":text,"text_elements":[]}]}),
        )
    }
}

impl Drop for Rpc {
    fn drop(&mut self) {
        self.input.take(); // EOF normally shuts down this owned, unloaded helper.
        let deadline = Instant::now() + Duration::from_secs(3);
        loop {
            if matches!(self.child.try_wait(), Ok(Some(_))) {
                return;
            }
            if Instant::now() >= deadline {
                break;
            }
            std::thread::sleep(Duration::from_millis(30));
        }
        let _ = nix::sys::signal::kill(
            nix::unistd::Pid::from_raw(self.child.id() as i32),
            nix::sys::signal::Signal::SIGTERM,
        );
        let deadline = Instant::now() + Duration::from_secs(3);
        while Instant::now() < deadline {
            if matches!(self.child.try_wait(), Ok(Some(_))) {
                return;
            }
            std::thread::sleep(Duration::from_millis(30));
        }
        eprintln!(
            "codexm gui: owned queue helper {} did not exit after EOF/SIGTERM",
            self.child.id()
        );
    }
}
