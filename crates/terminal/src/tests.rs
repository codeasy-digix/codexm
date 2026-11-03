use crate::{
    model::{Action, App, Preferences, Snapshot, View, clean},
    ui,
};
use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use serde_json::json;
use std::os::unix::fs::{PermissionsExt, symlink};
use std::{
    fs,
    path::PathBuf,
    time::{SystemTime, UNIX_EPOCH},
};

fn app() -> App {
    let mut app = App::new(Preferences::default());
    app.replace(
        serde_json::from_value(json!({"sessions":[
        {"id":"one","title":"한글 작업","cwd":"/project","account":"beta"},
        {"id":"two","title":"다른 작업","cwd":"/project"},
        {"id":"three","title":"보관된 작업","cwd":"/project","archived":true,"account":"removed"}
    ],"accounts":[{"name":"alpha"},{"name":"beta","default":true}]}))
        .unwrap(),
    );
    app
}
fn key(app: &mut App, code: KeyCode) -> Action {
    app.key(KeyEvent::new(code, KeyModifiers::NONE))
}

#[test]
fn enter_directly_hands_off_last_registered_account_or_default() {
    let mut app = app();
    assert!(
        matches!(key(&mut app,KeyCode::Enter),Action::Resume{thread,account} if thread=="one"&&account=="beta")
    );
    key(&mut app, KeyCode::Down);
    assert!(
        matches!(key(&mut app,KeyCode::Enter),Action::Resume{thread,account} if thread=="two"&&account=="default")
    );
    key(&mut app, KeyCode::Char('a'));
    key(&mut app, KeyCode::End);
    assert!(
        matches!(key(&mut app,KeyCode::Enter),Action::Resume{thread,account} if thread=="three"&&account=="default")
    );
}
#[test]
fn right_changes_only_this_launch_left_cancels_and_refresh_keeps_choice() {
    let mut app = app();
    key(&mut app, KeyCode::Right);
    assert!(app.view == View::Detail);
    assert_eq!(app.detail_account, 2);
    key(&mut app, KeyCode::Up);
    key(&mut app, KeyCode::Left);
    assert_eq!(app.preferred(app.chat().unwrap()), "beta");
    key(&mut app, KeyCode::Right);
    key(&mut app, KeyCode::Up);
    assert!(matches!(key(&mut app, KeyCode::Enter), Action::None));
    assert!(app.view == View::Chats);
    assert_eq!(app.preferred(app.chat().unwrap()), "alpha");
    let mut refreshed:Snapshot=serde_json::from_value(json!({"sessions":[{"id":"one","title":"한글 작업","account":"beta"}],"accounts":[{"name":"alpha"},{"name":"beta"}]})).unwrap();
    app.replace(refreshed);
    assert_eq!(app.preferred(app.chat().unwrap()), "alpha");
    refreshed=serde_json::from_value(json!({"sessions":[{"id":"one","title":"한글 작업","account":"beta"}],"accounts":[{"name":"beta"}]})).unwrap();
    app.replace(refreshed);
    assert_eq!(app.preferred(app.chat().unwrap()), "default");
}
#[test]
fn settings_account_mutations_require_confirmation_and_choose_auth_mode() {
    let mut app = app();
    key(&mut app, KeyCode::F(2));
    assert!(app.view == View::Settings);
    key(&mut app, KeyCode::Enter);
    assert!(app.view == View::Accounts);
    key(&mut app, KeyCode::Delete);
    assert!(app.view == View::Confirm);
    assert!(!app.confirm_yes);
    assert!(matches!(key(&mut app, KeyCode::Enter), Action::None));
    assert!(app.view == View::Accounts);
    key(&mut app, KeyCode::Delete);
    key(&mut app, KeyCode::Right);
    assert!(
        matches!(key(&mut app,KeyCode::Enter),Action::Request{method:"accounts.remove",params} if params["account"]=="alpha")
    );
    app.view = View::Accounts;
    key(&mut app, KeyCode::Char('a'));
    assert!(app.view == View::Nickname);
    for ch in "new-user".chars() {
        key(&mut app, KeyCode::Char(ch));
    }
    key(&mut app, KeyCode::Enter);
    assert!(app.view == View::AuthMode);
    assert!(
        matches!(key(&mut app,KeyCode::Enter),Action::Request{method:"login.start",params} if params["mode"]=="device"&&params["account"]=="new-user")
    );
    app.reply(
        "login.start",
        &json!({}),
        Ok(json!({"status":"pending","login_id":"private-fixture","user_code":"DUMMY"})),
    );
    assert!(
        matches!(key(&mut app,KeyCode::Esc),Action::Request{method:"login.cancel",params} if params["login_id"]=="private-fixture")
    );
}

#[test]
fn second_right_opens_chat_management_and_left_preserves_pending_account() {
    let mut app = app();
    key(&mut app, KeyCode::Right);
    key(&mut app, KeyCode::Up);
    key(&mut app, KeyCode::Right);
    assert!(app.view == View::ChatManage);
    assert_eq!(app.managed_chat.as_ref().unwrap().id, "one");
    assert_eq!(app.preferred(app.chat().unwrap()), "beta");
    key(&mut app, KeyCode::Left);
    assert!(app.view == View::Detail);
    assert_eq!(app.detail_account, 1);
    key(&mut app, KeyCode::Enter);
    assert_eq!(app.preferred(app.chat().unwrap()), "alpha");
}

#[test]
fn archive_confirmation_targets_the_original_chat_across_refresh() {
    let mut app = app();
    key(&mut app, KeyCode::Right);
    key(&mut app, KeyCode::Right);
    key(&mut app, KeyCode::Enter);
    assert!(app.view == View::ChatConfirm);
    assert!(!app.confirm_yes);
    assert!(matches!(key(&mut app, KeyCode::Enter), Action::None));
    assert!(app.view == View::ChatManage);
    key(&mut app, KeyCode::Enter);
    // A different row occupies the selected list position while confirming.
    let mut snapshot = app.snapshot.sessions.clone();
    snapshot.remove(0);
    app.replace(Snapshot {
        sessions: snapshot,
        ..Snapshot::default()
    });
    key(&mut app, KeyCode::Right);
    assert!(
        matches!(key(&mut app, KeyCode::Enter), Action::Request{method:"chats.archive",params} if params["thread_id"]=="one")
    );
    app.reply(
        "chats.archive",
        &json!({"thread_id":"one"}),
        Err("owner busy".into()),
    );
    assert!(app.view == View::ChatManage);
    assert!(app.refresh_needed);
    assert!(matches!(key(&mut app, KeyCode::Enter), Action::None));
    assert!(app.notice.contains("더 이상"));
    key(&mut app, KeyCode::Left);
    assert!(app.view == View::Chats);
}

#[test]
fn archive_success_hides_active_row_and_restore_uses_same_id() {
    let mut app = app();
    app.reply(
        "chats.archive",
        &json!({"thread_id":"one"}),
        Ok(json!({"thread_id":"one","archived":true,"changed":true})),
    );
    assert!(app.view == View::Chats);
    assert!(
        !app.filtered
            .iter()
            .any(|i| app.snapshot.sessions[*i].id == "one")
    );
    key(&mut app, KeyCode::Char('a'));
    key(&mut app, KeyCode::Home);
    key(&mut app, KeyCode::Right);
    key(&mut app, KeyCode::Right);
    assert_eq!(app.chat_action, 1);
    key(&mut app, KeyCode::Enter);
    assert!(app.view == View::ChatConfirm);
    assert!(!app.confirm_yes);
    key(&mut app, KeyCode::Right);
    assert!(
        matches!(key(&mut app, KeyCode::Enter), Action::Request{method:"chats.unarchive",params} if params["thread_id"]=="one")
    );
    app.reply(
        "chats.unarchive",
        &json!({"thread_id":"one"}),
        Ok(json!({"thread_id":"one","archived":false,"changed":true})),
    );
    assert!(app.view == View::Chats);
    assert!(!app.snapshot.sessions[0].archived);
}
#[test]
fn background_reads_allow_navigation_but_pending_mutations_do_not() {
    let mut app = app();
    app.request_started("snapshot");
    key(&mut app, KeyCode::F(2));
    key(&mut app, KeyCode::Enter);
    key(&mut app, KeyCode::Down);
    key(&mut app, KeyCode::Char('l'));
    assert!(app.view == View::AuthMode);
    assert_eq!(app.nickname, "beta");
    app.request_started("login.start");
    assert!(matches!(key(&mut app, KeyCode::Enter), Action::None));
    assert!(matches!(key(&mut app, KeyCode::Char('q')), Action::Quit));
    app.view = View::Login;
    app.request_started("login.status");
    assert!(matches!(
        key(&mut app, KeyCode::Esc),
        Action::Request {
            method: "login.cancel",
            ..
        }
    ));
}
#[test]
fn search_enter_cannot_resume_and_empty_lists_are_safe() {
    let mut app = app();
    key(&mut app, KeyCode::Char('/'));
    for ch in "다른".chars() {
        key(&mut app, KeyCode::Char(ch));
    }
    assert_eq!(app.filtered.len(), 1);
    assert!(matches!(key(&mut app, KeyCode::Enter), Action::None));
    assert!(!app.searching);
    key(&mut app, KeyCode::Char('/'));
    key(&mut app, KeyCode::Char('x'));
    key(&mut app, KeyCode::Esc);
    assert!(matches!(key(&mut app, KeyCode::Enter), Action::None));
    key(&mut app, KeyCode::Right);
    assert!(app.view == View::Chats);
}
#[test]
fn renderer_survives_small_terminals_and_filters_terminal_escape_sequences() {
    let mut app = app();
    app.snapshot.sessions[0].title = "한글\x1b]52;secret\x07\u{202e}title".into();
    assert!(!clean(&app.snapshot.sessions[0].title).contains('\x1b'));
    for (width, height) in [(120, 35), (80, 24), (40, 12), (20, 8)] {
        let mut terminal =
            ratatui::Terminal::new(ratatui::backend::TestBackend::new(width, height)).unwrap();
        for view in [
            View::Chats,
            View::Detail,
            View::ChatManage,
            View::ChatConfirm,
            View::Settings,
            View::Accounts,
            View::Nickname,
            View::AuthMode,
            View::Login,
            View::Confirm,
        ] {
            app.view = view;
            app.managed_chat = Some(app.snapshot.sessions[0].clone());
            app.confirm_chat = Some(crate::model::ChatArchiveChange {
                chat: app.snapshot.sessions[0].clone(),
                archived: true,
            });
            terminal.draw(|f| ui::draw(f, &app)).unwrap();
        }
    }
}

struct Temp(PathBuf);
impl Temp {
    fn new() -> Self {
        let path = std::env::temp_dir().join(format!(
            "codexm-terminal-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir(&path).unwrap();
        Self(path)
    }
}
impl Drop for Temp {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}
#[test]
fn preferences_touch_only_own_file_and_reject_links() {
    let temp = Temp::new();
    let native = temp.0.join("config.toml");
    fs::write(&native, "model='preserve'").unwrap();
    let file = temp.0.join("tui.json");
    let prefs = Preferences {
        include_archived: true,
    };
    prefs.save(&file).unwrap();
    assert!(Preferences::load(&file).unwrap().include_archived);
    assert_eq!(fs::read_to_string(&native).unwrap(), "model='preserve'");
    assert_eq!(
        fs::metadata(&file).unwrap().permissions().mode() & 0o777,
        0o600
    );
    fs::remove_file(&file).unwrap();
    symlink(&native, &file).unwrap();
    assert!(prefs.save(&file).is_err());
}

#[test]
fn owned_bridge_closes_after_one_request_without_replaying_it() {
    let temp = Temp::new();
    let binary = temp.0.join("bridge");
    fs::write(&binary,"#!/bin/sh\nIFS= read -r line\nprintf '%s\\n' '{\"protocolVersion\":1,\"id\":0,\"result\":{\"fixture\":true}}'\nwhile IFS= read -r line; do exit 9; done\n").unwrap();
    fs::set_permissions(&binary, fs::Permissions::from_mode(0o700)).unwrap();
    let mut bridge = crate::bridge::Bridge::start(&binary, &temp.0.join("config.toml")).unwrap();
    bridge.send("snapshot", json!({})).unwrap();
    let reply = bridge
        .replies
        .recv_timeout(std::time::Duration::from_secs(2))
        .unwrap();
    assert_eq!(reply.result.unwrap()["fixture"], true);
    bridge.stop();
    assert!(bridge.send("snapshot", json!({})).is_err());
}
