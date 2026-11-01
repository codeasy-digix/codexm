use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashMap;
use std::fs::{self, OpenOptions};
use std::io::{self, Write};
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt};
use std::path::Path;
use std::time::Instant;

#[derive(Clone, Default, Deserialize)]
pub struct Chat {
    pub id: String,
    pub title: String,
    #[serde(default)]
    pub cwd: String,
    #[serde(default)]
    pub account: String,
    #[serde(default)]
    pub archived: bool,
    #[serde(default)]
    pub model: String,
    #[serde(default)]
    pub effort: String,
    #[serde(default)]
    pub updated_at_ms: i64,
}
#[derive(Clone, Default, Deserialize)]
pub struct Account {
    pub name: String,
    #[serde(default)]
    pub default: bool,
    #[serde(default)]
    pub current: bool,
}
#[derive(Default, Deserialize)]
pub struct Snapshot {
    #[serde(default)]
    pub sessions: Vec<Chat>,
    #[serde(default)]
    pub accounts: Vec<Account>,
    #[serde(default)]
    pub warning: String,
    #[serde(default)]
    pub paths: Value,
}
#[derive(Default, Serialize, Deserialize)]
pub struct Preferences {
    #[serde(default)]
    pub include_archived: bool,
}
impl Preferences {
    pub fn load(file: &Path) -> io::Result<Self> {
        match fs::read(file) {
            Ok(data) => serde_json::from_slice(&data).map_err(io::Error::other),
            Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(Self::default()),
            Err(e) => Err(e),
        }
    }
    pub fn save(&self, file: &Path) -> io::Result<()> {
        let parent = file
            .parent()
            .ok_or_else(|| io::Error::other("settings directory missing"))?;
        fs::DirBuilder::new()
            .recursive(true)
            .mode(0o700)
            .create(parent)?;
        if fs::symlink_metadata(file).is_ok_and(|m| !m.is_file()) {
            return Err(io::Error::other("settings must be a regular file"));
        }
        let temp = file.with_extension(format!("tmp-{}", std::process::id()));
        let mut out = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&temp)?;
        let result = (|| {
            serde_json::to_writer_pretty(&mut out, self)?;
            out.write_all(b"\n")?;
            out.sync_all()?;
            fs::rename(&temp, file)
        })();
        let _ = fs::remove_file(temp);
        result
    }
}
#[derive(Clone, PartialEq)]
pub enum View {
    Chats,
    Detail,
    Settings,
    Accounts,
    Nickname,
    AuthMode,
    Login,
    Confirm,
}
pub enum Action {
    None,
    Quit,
    Resume { thread: String, account: String },
    Request { method: &'static str, params: Value },
    SavePreferences,
    OpenURL(String),
}
pub struct App {
    pub snapshot: Snapshot,
    pub selected: usize,
    pub filtered: Vec<usize>,
    pub query: String,
    pub searching: bool,
    pub view: View,
    pub detail_account: usize,
    pub account_row: usize,
    pub setting_row: usize,
    pub overrides: HashMap<String, String>,
    pub preferences: Preferences,
    pub archived: bool,
    pub nickname: String,
    pub auth_mode: usize,
    pub login: Value,
    pub confirm_method: &'static str,
    pub confirm_name: String,
    pub confirm_yes: bool,
    pub usage: HashMap<String, Value>,
    pub notice: String,
    pub busy: Option<Instant>,
    pub blocking: bool,
    pub refresh_needed: bool,
}
pub fn clean(s: &str) -> String {
    s.chars()
        .map(|c| {
            if c.is_control() || matches!(c,'\u{202a}'..='\u{202e}'|'\u{2066}'..='\u{2069}') {
                ' '
            } else {
                c
            }
        })
        .collect()
}
impl App {
    pub fn new(preferences: Preferences) -> Self {
        let archived = preferences.include_archived;
        Self {
            snapshot: Snapshot::default(),
            selected: 0,
            filtered: vec![],
            query: String::new(),
            searching: false,
            view: View::Chats,
            detail_account: 0,
            account_row: 0,
            setting_row: 0,
            overrides: HashMap::new(),
            preferences,
            archived,
            nickname: String::new(),
            auth_mode: 0,
            login: Value::Null,
            confirm_method: "",
            confirm_name: String::new(),
            confirm_yes: false,
            usage: HashMap::new(),
            notice: "목록을 불러옵니다…".into(),
            busy: None,
            blocking: false,
            refresh_needed: false,
        }
    }
    pub fn filter(&mut self) {
        let query = self.query.to_lowercase();
        self.filtered = self
            .snapshot
            .sessions
            .iter()
            .enumerate()
            .filter(|(_, s)| {
                (self.archived || !s.archived)
                    && format!("{} {} {} {}", s.title, s.cwd, s.id, s.account)
                        .to_lowercase()
                        .contains(&query)
            })
            .map(|(i, _)| i)
            .collect();
        self.selected = self.selected.min(self.filtered.len().saturating_sub(1));
    }
    pub fn chat(&self) -> Option<&Chat> {
        self.filtered
            .get(self.selected)
            .and_then(|i| self.snapshot.sessions.get(*i))
    }
    pub fn preferred(&self, chat: &Chat) -> String {
        let desired = self.overrides.get(&chat.id).unwrap_or(&chat.account);
        if desired == "default" || self.snapshot.accounts.iter().any(|a| a.name == *desired) {
            desired.clone()
        } else {
            "default".into()
        }
    }
    pub fn account_names(&self) -> Vec<String> {
        std::iter::once("default".into())
            .chain(self.snapshot.accounts.iter().map(|a| a.name.clone()))
            .collect()
    }
    pub fn replace(&mut self, snapshot: Snapshot) {
        let id = self.chat().map(|c| c.id.clone());
        let detail = self.account_names().get(self.detail_account).cloned();
        let account = self
            .snapshot
            .accounts
            .get(self.account_row)
            .map(|a| a.name.clone());
        self.snapshot = snapshot;
        self.filter();
        if let Some(id) = id
            && let Some(i) = self
                .filtered
                .iter()
                .position(|i| self.snapshot.sessions[*i].id == id)
        {
            self.selected = i;
        }
        self.account_row = account
            .and_then(|name| self.snapshot.accounts.iter().position(|a| a.name == name))
            .unwrap_or(
                self.account_row
                    .min(self.snapshot.accounts.len().saturating_sub(1)),
            );
        self.detail_account = detail
            .and_then(|name| self.account_names().iter().position(|n| *n == name))
            .unwrap_or(0);
    }
    pub fn reply(&mut self, method: &str, params: &Value, result: Result<Value, String>) {
        self.busy = None;
        self.blocking = false;
        match result {
            Err(e) => {
                self.notice = format!("오류: {}", clean(&e));
                if method == "login.start" {
                    self.view = View::AuthMode;
                }
            }
            Ok(value) => match method {
                "snapshot" => match serde_json::from_value(value) {
                    Ok(snapshot) => {
                        self.replace(snapshot);
                        if !self.snapshot.warning.is_empty() || self.notice == "목록을 불러옵니다…"
                        {
                            self.notice = self.snapshot.warning.clone();
                        }
                    }
                    Err(_) => {
                        self.notice =
                            "목록 응답을 읽지 못했습니다. 같은 버전으로 다시 설치하세요.".into()
                    }
                },
                "accounts.refresh" => {
                    if let Some(name) = params["account"].as_str() {
                        self.usage.insert(name.into(), value);
                    }
                    self.notice = "사용량을 확인했습니다.".into();
                }
                "accounts.remove" | "accounts.setDefault" => {
                    self.view = View::Accounts;
                    self.refresh_needed = true;
                    self.notice = if method == "accounts.remove" {
                        "등록 해제 완료 · 인증·대화·기본 인증·실행 중 작업은 보존했습니다."
                    } else {
                        "기본 계정 지정 완료 · 실행 중인 GUI에는 재실행 후 적용됩니다."
                    }
                    .into();
                }
                "login.start" | "login.status" | "login.cancel" => {
                    self.login = value;
                    match self.login["status"].as_str().unwrap_or("") {
                        "succeeded" => {
                            self.view = View::Accounts;
                            self.refresh_needed = true;
                            self.notice = "로그인 완료 · 계정이 등록되었습니다.".into();
                        }
                        "canceled" => {
                            self.view = View::Accounts;
                            self.notice = "로그인을 취소했습니다. 기존 인증은 보존했습니다.".into();
                        }
                        "failed" | "expired" => {
                            self.view = View::AuthMode;
                            self.notice = clean(
                                self.login["error"]
                                    .as_str()
                                    .unwrap_or("로그인이 만료됐습니다. 다시 시도하세요."),
                            );
                        }
                        _ => self.view = View::Login,
                    }
                }
                _ => {}
            },
        }
    }
    pub fn request_started(&mut self, method: &str) {
        self.busy = Some(Instant::now());
        self.blocking = !matches!(method, "snapshot" | "accounts.refresh" | "login.status");
    }
}
