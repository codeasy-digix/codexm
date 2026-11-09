use crate::model::{Action, App, ChatArchiveChange, View};
use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use serde_json::json;

fn request(method: &'static str, params: serde_json::Value) -> Action {
    Action::Request { method, params }
}
impl App {
    pub fn key(&mut self, key: KeyEvent) -> Action {
        if key.code == KeyCode::Char('c') && key.modifiers.contains(KeyModifiers::CONTROL) {
            return Action::Quit;
        }
        if self.searching {
            match key.code {
                KeyCode::Esc | KeyCode::Enter => self.searching = false,
                KeyCode::Backspace => {
                    self.query.pop();
                }
                KeyCode::Char('u') if key.modifiers.contains(KeyModifiers::CONTROL) => {
                    self.query.clear()
                }
                KeyCode::Char(ch)
                    if !key
                        .modifiers
                        .intersects(KeyModifiers::CONTROL | KeyModifiers::ALT) =>
                {
                    self.query.push(ch)
                }
                _ => {}
            }
            self.selected = 0;
            self.filter();
            return Action::None;
        }
        if key.code == KeyCode::Char('q') && self.view != View::Nickname {
            return Action::Quit;
        }
        if key.code == KeyCode::F(2) && self.view != View::Login && !self.blocking {
            self.view = if self.view == View::Settings {
                View::Chats
            } else {
                View::Settings
            };
            return Action::None;
        }
        if self.blocking {
            return Action::None;
        }
        let mut key = key;
        if key.modifiers.is_empty()
            && matches!(self.view, View::Chats | View::Detail | View::ChatManage)
        {
            key.code = match key.code {
                KeyCode::Char('i') => KeyCode::Up,
                KeyCode::Char('j') => KeyCode::Left,
                KeyCode::Char('k') => KeyCode::Down,
                KeyCode::Char('l') => KeyCode::Right,
                code => code,
            };
        }
        match self.view {
            View::Chats => match key.code {
                KeyCode::Up => self.selected = self.selected.saturating_sub(1),
                KeyCode::Down => {
                    self.selected = (self.selected + 1).min(self.filtered.len().saturating_sub(1))
                }
                KeyCode::PageUp => self.selected = self.selected.saturating_sub(10),
                KeyCode::PageDown => {
                    self.selected = (self.selected + 10).min(self.filtered.len().saturating_sub(1))
                }
                KeyCode::Home => self.selected = 0,
                KeyCode::End => self.selected = self.filtered.len().saturating_sub(1),
                KeyCode::Char('/') => self.searching = true,
                KeyCode::Char('a') => {
                    self.archived = !self.archived;
                    self.filter();
                }
                KeyCode::Char('r') => return request("snapshot", json!({})),
                KeyCode::Esc => {
                    self.query.clear();
                    self.filter();
                }
                KeyCode::Right => {
                    if let Some(chat) = self.chat() {
                        let name = self.preferred(chat);
                        self.detail_account = self
                            .account_names()
                            .iter()
                            .position(|n| *n == name)
                            .unwrap_or(0);
                        self.view = View::Detail;
                    }
                }
                KeyCode::Enter => {
                    if let Some(chat) = self.chat() {
                        return Action::Resume {
                            thread: chat.id.clone(),
                            account: self.preferred(chat),
                        };
                    }
                }
                _ => {}
            },
            View::Detail => match key.code {
                KeyCode::Left | KeyCode::Esc => self.view = View::Chats,
                KeyCode::Right => {
                    if let Some(chat) = self.chat().cloned() {
                        self.chat_action = usize::from(chat.archived);
                        self.managed_chat = Some(chat);
                        self.confirm_chat = None;
                        self.view = View::ChatManage;
                    }
                }
                KeyCode::Up => self.detail_account = self.detail_account.saturating_sub(1),
                KeyCode::Down => {
                    self.detail_account =
                        (self.detail_account + 1).min(self.snapshot.accounts.len())
                }
                KeyCode::Enter => {
                    if let Some(chat) = self.chat() {
                        self.overrides.insert(
                            chat.id.clone(),
                            self.account_names()[self.detail_account].clone(),
                        );
                        self.notice = "이번 실행 계정을 선택했습니다. Enter로 재개하세요.".into();
                    }
                    self.view = View::Chats;
                }
                _ => {}
            },
            View::ChatManage => match key.code {
                KeyCode::Left | KeyCode::Esc => self.leave_chat_management(),
                KeyCode::Up => self.chat_action = self.chat_action.saturating_sub(1),
                KeyCode::Down => self.chat_action = (self.chat_action + 1).min(1),
                KeyCode::Enter => {
                    if let Some(chat) = &self.managed_chat {
                        let current = self.snapshot.sessions.iter().find(|c| c.id == chat.id);
                        if let Some(chat) = current {
                            let archived = self.chat_action == 0;
                            if chat.archived == archived {
                                self.notice = if archived {
                                    "이미 보관된 채팅입니다."
                                } else {
                                    "보관되지 않은 채팅입니다."
                                }
                                .into();
                            } else {
                                self.confirm_chat = Some(ChatArchiveChange {
                                    chat: chat.clone(),
                                    archived,
                                });
                                self.confirm_yes = false;
                                self.view = View::ChatConfirm;
                            }
                        } else {
                            self.notice = "선택한 채팅이 더 이상 목록에 없습니다. ←로 돌아가 다시 선택하세요.".into();
                        }
                    }
                }
                _ => {}
            },
            View::ChatConfirm => match key.code {
                KeyCode::Esc => {
                    self.confirm_chat = None;
                    self.view = View::ChatManage;
                }
                KeyCode::Left | KeyCode::Right | KeyCode::Tab => {
                    self.confirm_yes = !self.confirm_yes
                }
                KeyCode::Enter => {
                    if self.confirm_yes {
                        if let Some(change) = &self.confirm_chat {
                            return request(
                                if change.archived {
                                    "chats.archive"
                                } else {
                                    "chats.unarchive"
                                },
                                json!({"thread_id":change.chat.id}),
                            );
                        }
                    } else {
                        self.confirm_chat = None;
                        self.view = View::ChatManage;
                    }
                }
                _ => {}
            },
            View::Settings => match key.code {
                KeyCode::Esc | KeyCode::Left => self.view = View::Chats,
                KeyCode::Up => self.setting_row = self.setting_row.saturating_sub(1),
                KeyCode::Down => self.setting_row = (self.setting_row + 1).min(1),
                KeyCode::Enter | KeyCode::Right => {
                    if self.setting_row == 0 {
                        self.view = View::Accounts;
                    } else {
                        self.preferences.include_archived = !self.preferences.include_archived;
                        self.archived = self.preferences.include_archived;
                        self.filter();
                        return Action::SavePreferences;
                    }
                }
                _ => {}
            },
            View::Accounts => match key.code {
                KeyCode::Esc | KeyCode::Left => self.view = View::Settings,
                KeyCode::Up => self.account_row = self.account_row.saturating_sub(1),
                KeyCode::Down => {
                    self.account_row =
                        (self.account_row + 1).min(self.snapshot.accounts.len().saturating_sub(1))
                }
                KeyCode::Char('a') => {
                    self.nickname.clear();
                    self.auth_mode = 0;
                    self.view = View::Nickname;
                }
                KeyCode::Char('r') => return request("snapshot", json!({})),
                KeyCode::Char('l') => {
                    if let Some(account) = self.snapshot.accounts.get(self.account_row) {
                        self.nickname = account.name.clone();
                        self.auth_mode = 0;
                        self.view = View::AuthMode;
                    }
                }
                KeyCode::Char('u') => {
                    if let Some(account) = self.snapshot.accounts.get(self.account_row) {
                        return request("accounts.refresh", json!({"account":account.name}));
                    }
                }
                KeyCode::Char('d') | KeyCode::Delete | KeyCode::Char('x') => {
                    if let Some(account) = self.snapshot.accounts.get(self.account_row) {
                        self.confirm_name = account.name.clone();
                        self.confirm_method = if key.code == KeyCode::Char('d') {
                            "accounts.setDefault"
                        } else {
                            "accounts.remove"
                        };
                        self.confirm_yes = false;
                        self.view = View::Confirm;
                    }
                }
                _ => {}
            },
            View::Nickname => match key.code {
                KeyCode::Esc => self.view = View::Accounts,
                KeyCode::Backspace => {
                    self.nickname.pop();
                }
                KeyCode::Char(ch)
                    if (ch.is_ascii_alphanumeric() || ch == '_' || ch == '-')
                        && self.nickname.len() < 64 =>
                {
                    self.nickname.push(ch)
                }
                KeyCode::Enter => {
                    if self.nickname != "default"
                        && self
                            .nickname
                            .starts_with(|c: char| c.is_ascii_alphanumeric())
                    {
                        self.view = View::AuthMode;
                    } else {
                        self.notice="영문/숫자로 시작하는 닉네임을 입력하세요. default는 예약된 이름입니다.".into();
                    }
                }
                _ => {}
            },
            View::AuthMode => match key.code {
                KeyCode::Esc | KeyCode::Left => self.view = View::Accounts,
                KeyCode::Up | KeyCode::Down => self.auth_mode = 1 - self.auth_mode,
                KeyCode::Enter => {
                    self.view = View::Login;
                    self.login = json!({"status":"starting"});
                    return request(
                        "login.start",
                        json!({"account":self.nickname,"mode":if self.auth_mode==0{"device"}else{"browser"}}),
                    );
                }
                _ => {}
            },
            View::Login => match key.code {
                KeyCode::Esc => {
                    return request("login.cancel", json!({"login_id":self.login["login_id"]}));
                }
                KeyCode::Char('o') => {
                    if let Some(url) = self.login["verification_url"]
                        .as_str()
                        .or(self.login["auth_url"].as_str())
                    {
                        return Action::OpenURL(url.into());
                    }
                }
                _ => {}
            },
            View::Confirm => match key.code {
                KeyCode::Esc => self.view = View::Accounts,
                KeyCode::Left | KeyCode::Right | KeyCode::Tab => {
                    self.confirm_yes = !self.confirm_yes
                }
                KeyCode::Enter => {
                    if self.confirm_yes {
                        return request(self.confirm_method, json!({"account":self.confirm_name}));
                    } else {
                        self.view = View::Accounts;
                    }
                }
                _ => {}
            },
        }
        Action::None
    }
}
