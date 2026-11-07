use crate::desktop::{Desktop, Owner};
use crate::receipt::{Receipt, Store, message_hash, valid_uuid};
use crate::rpc::{Failure, Rpc};
use serde_json::{Value, json};
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, Instant};

pub trait Backend {
    fn read(&mut self, method: &str, params: Value) -> Result<Value, String>;
    fn open(&mut self, thread: &str) -> Result<Owner, String>;
    fn owner(&mut self, thread: &str) -> Result<Option<Owner>, String>;
    fn enqueue(&mut self, thread: &str, request: &str, text: &str) -> Result<Value, Failure>;
}

pub struct Local<'a> {
    pub desktop: &'a Desktop,
    pub rpc: Rpc,
}
impl Backend for Local<'_> {
    fn read(&mut self, method: &str, params: Value) -> Result<Value, String> {
        self.rpc.read(method, params)
    }
    fn open(&mut self, thread: &str) -> Result<Owner, String> {
        self.desktop.open_thread(thread)
    }
    fn owner(&mut self, thread: &str) -> Result<Option<Owner>, String> {
        self.desktop.owner(thread)
    }
    fn enqueue(&mut self, thread: &str, request: &str, text: &str) -> Result<Value, Failure> {
        self.rpc.enqueue(thread, request, text)
    }
}

pub fn active_chat<B: Backend>(backend: &mut B, id: &str) -> Result<Value, String> {
    let r = backend.read("thread/read", json!({"threadId":id,"includeTurns":false}))?;
    if r["thread"]["id"].as_str() != Some(id) {
        return Err("selected chat is not in this desktop's native home".into());
    }
    let mut cursor = Value::Null;
    for _ in 0..50 {
        let page = backend.read("thread/list", json!({"limit":100,"archived":false,"useStateDbOnly":true,
            "sortKey":"updated_at","searchTerm":r["thread"]["name"],"cursor":cursor,
            "sourceKinds":["cli","vscode","exec","appServer","subAgent","subAgentReview","subAgentCompact","subAgentThreadSpawn","subAgentOther","unknown"]}))?;
        let data = page["data"]
            .as_array()
            .ok_or("invalid native chat catalog response")?;
        if data.iter().any(|t| t["id"].as_str() == Some(id)) {
            return Ok(r["thread"].clone());
        }
        let next = page.get("nextCursor").cloned().unwrap_or(Value::Null);
        if next.is_null() || next == cursor {
            break;
        }
        cursor = next;
    }
    Err("chat is archived or could not be verified in the active catalog; restore it in the GUI first (no input sent)".into())
}

pub struct Submission<'a> {
    pub thread: &'a str,
    pub request: &'a str,
    pub text: &'a str,
    pub wait: Duration,
}

pub fn submit<B: Backend>(
    backend: &mut B,
    store: &Store,
    s: Submission<'_>,
    cancelled: &AtomicBool,
) -> Result<Receipt, String> {
    if !valid_uuid(s.thread) || !valid_uuid(s.request) {
        return Err("chat and request IDs must be lowercase UUIDs".into());
    }
    if s.text.trim().is_empty() || s.text.len() > 65536 {
        return Err("message must contain 1–65536 UTF-8 bytes".into());
    }
    if let Some(r) = store.read(s.request)? {
        if r.thread_id != s.thread || r.message_sha256 != message_hash(s.text) {
            return Err("request ID already belongs to different input; no request sent".into());
        }
        return Ok(r); // No retry, even if a previous process died before recording its ACK.
    }
    let mut r = Receipt::new(s.thread, s.request, s.text);
    store.reserve(&r)?;
    let prepare: Result<Owner, String> = (|| {
        active_chat(backend, s.thread)?;
        let owner = backend.open(s.thread)?;
        if backend.owner(s.thread)?.as_ref() != Some(&owner) {
            return Err("GUI writer changed before submission; no input sent".into());
        }
        if cancelled.load(Ordering::Relaxed) {
            return Err("cancelled before submission".into());
        }
        Ok(owner)
    })();
    let owner = match prepare {
        Ok(owner) => owner,
        Err(e) => {
            r.outcome("not_sent", &e);
            store.save(&r)?;
            return Ok(r);
        }
    };
    r.owner = Some(owner);
    r.outcome(
        "sending",
        "Submission may have reached the native queue; do not resend this request.",
    );
    store.save(&r)?; // Durable before the only mutation in this controller.
    match backend.enqueue(s.thread, s.request, s.text) {
        Ok(ack) if ack["queuedSubmission"]["clientUserMessageId"].as_str() == Some(s.request)
            && ack["queuedSubmission"]["id"].as_str().is_some_and(valid_uuid) => {
                r.queue_id = ack["queuedSubmission"]["id"].as_str().map(str::to_owned);
                r.outcome("accepted", "Native queue accepted the request. Acceptance is not completion; it runs after the current turn.");
            }
        Ok(_) => r.outcome("unknown", "Queue acknowledgement did not match the request; inspect status, never resend automatically."),
        Err(Failure::Rejected(e)) => r.outcome("rejected", &e),
        Err(Failure::Uncertain(e)) => r.outcome("unknown", &e),
    }
    store.save(&r).map_err(|_| {
        format!(
            "request {} may be accepted, but its receipt update failed; do not resend",
            s.request
        )
    })?;
    if r.state != "accepted" {
        return Ok(r);
    }
    let deadline = Instant::now() + s.wait;
    while Instant::now() < deadline && !cancelled.load(Ordering::Relaxed) {
        let observed = observe(backend, &r);
        if cancelled.load(Ordering::Relaxed) {
            // Cancellation of this observer does not mean the GUI changed owner.
            break;
        }
        r = observed;
        if ["completed", "failed", "owner_changed", "owner_unverified"].contains(&r.state.as_str())
        {
            break;
        }
        std::thread::sleep(Duration::from_millis(200));
    }
    store.save(&r).map_err(|_| {
        format!(
            "request {} was accepted, but its status save failed; do not resend",
            s.request
        )
    })?;
    Ok(r)
}

/// Read-only reconciliation by the native client ID, never by matching prompt text.
pub fn observe<B: Backend>(backend: &mut B, original: &Receipt) -> Receipt {
    let mut r = original.clone();
    if ["not_sent", "rejected", "completed"].contains(&r.state.as_str()) {
        return r;
    }
    if r.owner.is_none() {
        return r;
    }
    match backend.owner(&r.thread_id) {
        Ok(owner) if owner.as_ref() == r.owner.as_ref() => {}
        Ok(_) => {
            r.outcome("owner_changed", "GUI ownership is no longer verified. The native queue is durable and may be consumed by a later owner. No takeover, cancellation or resend was attempted.");
            return r;
        }
        Err(_) => {
            r.outcome("owner_unverified", "Current owner lookup failed. This does not prove the GUI stopped. No takeover, cancellation or resend was attempted.");
            return r;
        }
    }
    let mut cursor = Value::Null;
    for _ in 0..10 {
        let response = match backend.read("thread/turns/list", json!({"threadId":r.thread_id,"limit":20,"itemsView":"full","sortDirection":"desc","cursor":cursor})) {
            Ok(v) => v,
            Err(_) => { r.detail = "Status read unavailable. Preserve the recorded request; do not resend.".into(); return r; }
        };
        let Some(turns) = response["data"].as_array() else {
            return r;
        };
        for turn in turns {
            if turn["items"].as_array().is_some_and(|items| {
                items.iter().any(|item| {
                    item["type"] == "userMessage"
                        && item["clientId"].as_str() == Some(&r.request_id)
                })
            }) {
                r.turn_id = turn["id"].as_str().map(str::to_owned);
                let state = match turn["status"].as_str() {
                    Some("completed") => "completed",
                    Some("failed") if !turn["error"].is_null() => "failed",
                    _ => "started",
                };
                // A passive server reconstructs an unfinished legacy rollout as
                // "interrupted" even while its actual GUI owner is still running.
                // Only an explicit completed result or persisted error is final.
                r.outcome(state, if state == "started" {
                    "Matched this request's client ID. A passive read cannot distinguish an in-flight turn from an interruption; completion is not yet confirmed."
                } else {
                    "Matched this request's client ID and terminal result in the native transcript while the original GUI writer remained verified."
                });
                return r;
            }
        }
        let next = response.get("nextCursor").cloned().unwrap_or(Value::Null);
        if next.is_null() || next == cursor {
            break;
        }
        cursor = next;
    }
    let mut cursor = Value::Null;
    for _ in 0..10 {
        let Ok(response) = backend.read(
            "thread/queue/list",
            json!({"threadId":r.thread_id,"limit":100,"cursor":cursor}),
        ) else {
            return r;
        };
        let Some(queue) = response["data"].as_array() else {
            return r;
        };
        if let Some(item) = queue
            .iter()
            .find(|q| q["clientUserMessageId"].as_str() == Some(&r.request_id))
        {
            r.queue_id = item["id"].as_str().map(str::to_owned);
            r.outcome("accepted", "Request is still in the native queue. It has not been observed as a completed turn.");
            return r;
        }
        let next = response.get("nextCursor").cloned().unwrap_or(Value::Null);
        if next.is_null() || next == cursor {
            break;
        }
        cursor = next;
    }
    r.detail = "Not found in bounded queue/history reads; this is not proof of non-delivery. Do not resend.".into();
    r
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{desktop::Process, receipt::uuid};
    struct Fake {
        owner: Option<Owner>,
        replace: bool,
        calls: Vec<String>,
        submissions: usize,
        fail: bool,
        request: String,
        turns: Value,
    }
    impl Fake {
        fn new() -> Self {
            let process = Process {
                pid: 100,
                parent: 99,
                started: "incarnation-1".into(),
                executable: "/fixture/codex".into(),
            };
            Self {
                owner: Some(Owner {
                    gui: process.clone(),
                    writer: process,
                }),
                replace: false,
                calls: vec![],
                submissions: 0,
                fail: false,
                request: String::new(),
                turns: json!({"data":[]}),
            }
        }
    }
    impl Backend for Fake {
        fn read(&mut self, method: &str, params: Value) -> Result<Value, String> {
            self.calls.push(method.into());
            match method {
                "thread/read" => Ok(json!({"thread":{"id":params["threadId"],"name":"fixture"}})),
                "thread/list" => {
                    assert_eq!(params["useStateDbOnly"], true);
                    Ok(json!({"data":[{"id":self.request}]}))
                }
                "thread/turns/list" => Ok(self.turns.clone()),
                "thread/queue/list" => Ok(json!({"data":[]})),
                _ => panic!("unexpected method {method}"),
            }
        }
        fn open(&mut self, _: &str) -> Result<Owner, String> {
            self.owner
                .clone()
                .ok_or("external CLI owns the chat".into())
        }
        fn owner(&mut self, _: &str) -> Result<Option<Owner>, String> {
            if self.replace {
                let mut owner = self.owner.clone();
                owner.as_mut().unwrap().writer.started = "replacement".into();
                return Ok(owner);
            }
            Ok(self.owner.clone())
        }
        fn enqueue(&mut self, _: &str, request: &str, _: &str) -> Result<Value, Failure> {
            self.submissions += 1;
            if self.fail {
                return Err(Failure::Uncertain("lost acknowledgement".into()));
            }
            Ok(json!({"queuedSubmission":{"id":uuid().unwrap(),"clientUserMessageId":request}}))
        }
    }
    #[test]
    fn rejects_foreign_or_replaced_owner_without_mutation() {
        for replace in [false, true] {
            let dir = std::env::temp_dir().join(format!("codexm-gui-control-{}", uuid().unwrap()));
            let store = Store::open(dir.clone()).unwrap();
            let mut b = Fake::new();
            b.request = uuid().unwrap();
            let thread = b.request.clone();
            b.replace = replace;
            if !replace {
                b.owner = None;
            }
            let r = submit(
                &mut b,
                &store,
                Submission {
                    thread: &thread,
                    request: &uuid().unwrap(),
                    text: "fixture",
                    wait: Duration::ZERO,
                },
                &AtomicBool::new(false),
            )
            .unwrap();
            assert_eq!(r.state, "not_sent");
            assert_eq!(b.submissions, 0);
            std::fs::remove_dir_all(dir).unwrap();
        }
    }
    #[test]
    fn uncertain_request_is_never_replayed_and_status_matches_client_id() {
        let dir = std::env::temp_dir().join(format!("codexm-gui-control-{}", uuid().unwrap()));
        let store = Store::open(dir.clone()).unwrap();
        let mut b = Fake::new();
        b.request = uuid().unwrap();
        b.fail = true;
        let thread = b.request.clone();
        let request = uuid().unwrap();
        for _ in 0..2 {
            let r = submit(
                &mut b,
                &store,
                Submission {
                    thread: &thread,
                    request: &request,
                    text: "same text",
                    wait: Duration::ZERO,
                },
                &AtomicBool::new(false),
            )
            .unwrap();
            assert_eq!(r.state, "unknown");
        }
        assert_eq!(b.submissions, 1);
        let r = store.read(&request).unwrap().unwrap();
        b.turns = json!({"data":[{"id":"one","status":"completed","items":[{"type":"userMessage","clientId":"different","content":[{"text":"same text"}]}]}]});
        assert_ne!(observe(&mut b, &r).state, "completed");
        b.turns = json!({"data":[{"id":"two","status":"interrupted","error":null,"items":[{"type":"userMessage","clientId":request}]}]});
        assert_eq!(
            observe(&mut b, &r).state,
            "started",
            "a passive reader's in-flight interrupted snapshot is not terminal"
        );
        b.turns = json!({"data":[{"id":"two","status":"completed","items":[{"type":"userMessage","clientId":request}]}]});
        assert_eq!(observe(&mut b, &r).state, "completed");
        assert!(
            submit(
                &mut b,
                &store,
                Submission {
                    thread: &thread,
                    request: &request,
                    text: "different input",
                    wait: Duration::ZERO
                },
                &AtomicBool::new(false)
            )
            .is_err()
        );
        b.replace = true;
        assert_eq!(observe(&mut b, &r).state, "owner_changed");
        assert_eq!(b.submissions, 1);
        std::fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn cancelling_observer_keeps_acknowledged_submission() {
        struct Cancelling<'a> {
            inner: Fake,
            flag: &'a AtomicBool,
            owner_reads: usize,
        }
        impl Backend for Cancelling<'_> {
            fn read(&mut self, m: &str, p: Value) -> Result<Value, String> {
                self.inner.read(m, p)
            }
            fn open(&mut self, id: &str) -> Result<Owner, String> {
                self.inner.open(id)
            }
            fn owner(&mut self, id: &str) -> Result<Option<Owner>, String> {
                self.owner_reads += 1;
                if self.owner_reads > 1 {
                    self.flag.store(true, Ordering::Relaxed);
                    return Err("observer cancelled".into());
                }
                self.inner.owner(id)
            }
            fn enqueue(&mut self, id: &str, request: &str, text: &str) -> Result<Value, Failure> {
                self.inner.enqueue(id, request, text)
            }
        }
        let dir = std::env::temp_dir().join(format!("codexm-gui-control-{}", uuid().unwrap()));
        let store = Store::open(dir.clone()).unwrap();
        let flag = AtomicBool::new(false);
        let mut b = Cancelling {
            inner: Fake::new(),
            flag: &flag,
            owner_reads: 0,
        };
        b.inner.request = uuid().unwrap();
        let thread = b.inner.request.clone();
        let r = submit(
            &mut b,
            &store,
            Submission {
                thread: &thread,
                request: &uuid().unwrap(),
                text: "fixture",
                wait: Duration::from_secs(1),
            },
            &flag,
        )
        .unwrap();
        assert_eq!(r.state, "accepted");
        assert_eq!(b.inner.submissions, 1);
        assert!(flag.load(Ordering::Relaxed));
        std::fs::remove_dir_all(dir).unwrap();
    }
}
