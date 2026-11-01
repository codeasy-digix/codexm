//! One owned, model-free compatibility bridge. No web server or native writer.
use nix::{
    sys::signal::{Signal, kill},
    unistd::Pid,
};
use serde_json::{Value, json};
use std::io::{self, BufRead, BufReader, Read, Write};
use std::path::Path;
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::{
    Arc, Mutex,
    mpsc::{self, Receiver, SyncSender},
};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

#[derive(Clone)]
pub struct Request {
    pub method: String,
    pub params: Value,
}
pub struct Reply {
    pub request: Request,
    pub result: Result<Value, String>,
}
pub struct Bridge {
    requests: Option<SyncSender<Request>>,
    pub replies: Receiver<Reply>,
    child: Child,
    stdin: Arc<Mutex<Option<ChildStdin>>>,
    worker: Option<JoinHandle<()>>,
}
impl Bridge {
    pub fn start(binary: &Path, config: &Path) -> io::Result<Self> {
        let mut bytes = [0u8; 16];
        getrandom::fill(&mut bytes).map_err(|e| io::Error::other(e.to_string()))?;
        let owner = format!(
            "tui-{}",
            bytes.iter().map(|b| format!("{b:02x}")).collect::<String>()
        );
        let mut child = Command::new(binary)
            .args(["--config"])
            .arg(config)
            .args(["bridge", "--stdio"])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()?;
        let input = Arc::new(Mutex::new(child.stdin.take()));
        let worker_input = input.clone();
        let mut output = BufReader::new(
            child
                .stdout
                .take()
                .ok_or_else(|| io::Error::other("bridge output unavailable"))?,
        );
        let (tx, rx) = mpsc::sync_channel::<Request>(1);
        let (reply_tx, reply_rx) = mpsc::channel();
        let worker = thread::spawn(move || {
            for (index, request) in rx.into_iter().enumerate() {
                let operation = format!("{owner}-{index}");
                let payload = json!({"protocolVersion":1,"id":index,"owner":owner,"operation_id":operation,"method":request.method,"params":request.params});
                let result = (|| -> io::Result<Value> {
                    {
                        let mut guard = worker_input
                            .lock()
                            .map_err(|_| io::Error::other("bridge input closed"))?;
                        let stream = guard
                            .as_mut()
                            .ok_or_else(|| io::Error::other("bridge stopped"))?;
                        serde_json::to_writer(&mut *stream, &payload)?;
                        stream.write_all(b"\n")?;
                        stream.flush()?;
                    }
                    let mut line = Vec::new();
                    output
                        .by_ref()
                        .take(16 * 1024 * 1024 + 1)
                        .read_until(b'\n', &mut line)?;
                    if line.is_empty() || line.len() > 16 * 1024 * 1024 {
                        return Err(io::Error::other(
                            "bridge reply missing or too large; request was not retried",
                        ));
                    }
                    let reply: Value = serde_json::from_slice(&line)?;
                    if reply["protocolVersion"] != 1 || reply["id"] != index {
                        return Err(io::Error::other("bridge protocol mismatch"));
                    }
                    if let Some(error) = reply.get("error") {
                        return Err(io::Error::other(
                            error["message"].as_str().unwrap_or("request failed"),
                        ));
                    }
                    Ok(reply["result"].clone())
                })()
                .map_err(|e| e.to_string());
                if reply_tx.send(Reply { request, result }).is_err() {
                    break;
                }
            }
        });
        Ok(Self {
            requests: Some(tx),
            replies: reply_rx,
            child,
            stdin: input,
            worker: Some(worker),
        })
    }
    pub fn send(&self, method: &str, params: Value) -> io::Result<()> {
        self.requests
            .as_ref()
            .ok_or_else(|| io::Error::other("bridge stopped"))?
            .try_send(Request {
                method: method.into(),
                params,
            })
            .map_err(io::Error::other)
    }
    pub fn stop(&mut self) {
        self.requests.take();
        if let Ok(mut input) = self.stdin.lock() {
            input.take();
        }
        if matches!(self.child.try_wait(), Ok(None)) {
            let _ = kill(Pid::from_raw(self.child.id() as i32), Signal::SIGTERM);
            let start = Instant::now();
            while start.elapsed() < Duration::from_secs(3) {
                if !matches!(self.child.try_wait(), Ok(None)) {
                    break;
                }
                thread::sleep(Duration::from_millis(20));
            }
            if matches!(self.child.try_wait(), Ok(None)) {
                let _ = self.child.kill();
            }
        }
        let _ = self.child.wait();
        if let Some(worker) = self.worker.take() {
            let _ = worker.join();
        }
    }
}
impl Drop for Bridge {
    fn drop(&mut self) {
        self.stop();
    }
}
