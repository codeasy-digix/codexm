use crate::model::{App, View, clean};
use ratatui::{
    Frame,
    layout::{Constraint, Direction, Layout, Rect},
    style::{Color, Modifier, Style},
    text::Line,
    widgets::{Block, Borders, List, ListItem, ListState, Paragraph, Wrap},
};
use serde_json::Value;

const BLUE: Color = Color::LightBlue;
fn block(title: &str, active: bool) -> Block<'_> {
    Block::default()
        .title(title)
        .borders(Borders::ALL)
        .border_style(Style::default().fg(if active { BLUE } else { Color::DarkGray }))
}
fn list(
    frame: &mut Frame,
    area: Rect,
    title: &str,
    rows: Vec<ListItem>,
    selected: usize,
    active: bool,
) {
    let mut state = ListState::default().with_selected(Some(selected));
    frame.render_stateful_widget(
        List::new(rows)
            .block(block(title, active))
            .highlight_symbol("▶ ")
            .highlight_style(
                Style::default()
                    .bg(if active { Color::Blue } else { Color::DarkGray })
                    .add_modifier(Modifier::BOLD),
            ),
        area,
        &mut state,
    );
}
fn paragraph(frame: &mut Frame, area: Rect, title: &str, text: String) {
    frame.render_widget(
        Paragraph::new(text)
            .block(block(title, true))
            .wrap(Wrap { trim: false }),
        area,
    );
}
pub fn draw(frame: &mut Frame, app: &App) {
    let area = frame.area();
    if area.width < 35 || area.height < 10 {
        frame.render_widget(
            Paragraph::new("터미널을 35열 × 10행 이상으로 넓혀주세요.\nCtrl+C: 종료"),
            area,
        );
        return;
    }
    let slots = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(2),
            Constraint::Min(4),
            Constraint::Length(3),
            Constraint::Length(2),
        ])
        .split(area);
    frame.render_widget(
        Paragraph::new("codexm  ·  로컬 채팅과 계정  ·  F2 설정")
            .style(Style::default().fg(BLUE).add_modifier(Modifier::BOLD)),
        slots[0],
    );
    match app.view {
        View::Chats | View::Detail => chats(frame, slots[1], app),
        View::Settings => settings(frame, slots[1], app),
        View::Accounts => accounts(frame, slots[1], app),
        View::Nickname => paragraph(
            frame,
            slots[1],
            "계정 추가",
            format!(
                "닉네임: {}_\n\n영문·숫자·_·- 사용 가능\nEnter: 인증 방식 선택   Esc: 취소",
                clean(&app.nickname)
            ),
        ),
        View::AuthMode => list(
            frame,
            slots[1],
            &format!("{} · 인증 방식", clean(&app.nickname)),
            vec![
                ListItem::new("기기 코드 로그인 · SSH/다른 기기에서도 가능"),
                ListItem::new("브라우저 로그인 · Codex가 실행되는 장비의 브라우저"),
            ],
            app.auth_mode,
            true,
        ),
        View::Login => {
            let status = app.login["status"].as_str().unwrap_or("starting");
            let url = app.login["verification_url"]
                .as_str()
                .or(app.login["auth_url"].as_str())
                .unwrap_or("");
            paragraph(
                frame,
                slots[1],
                "계정 로그인",
                if status == "starting" || status == "verifying" {
                    format!(
                        "{}\n\n{}",
                        clean(&app.nickname),
                        if status == "starting" {
                            "공식 인증을 준비합니다…"
                        } else {
                            "로그인 완료를 검증합니다…"
                        }
                    )
                } else {
                    format!(
                        "계정: {}\n\n{}\n\n기기 코드: {}\n\nO: 이 장비의 브라우저로 열기\nEsc: 로그인 취소\n\nSSH로 접속했다면 URL/기기 코드를 자신의 브라우저에서 사용하세요.",
                        clean(&app.nickname),
                        clean(url),
                        clean(app.login["user_code"].as_str().unwrap_or("브라우저 인증"))
                    )
                },
            );
        }
        View::Confirm => paragraph(
            frame,
            slots[1],
            "확인",
            format!(
                "{}\n\n{}\n\n{}",
                clean(&app.confirm_name),
                if app.confirm_method == "accounts.remove" {
                    "계정 등록을 해제할까요?\n인증 파일·대화·기본 인증·실행 중 작업은 보존합니다."
                } else {
                    "이 계정을 기본 계정으로 지정할까요?\n새 기본 환경에 적용됩니다. 실행 중 GUI는 재실행 후 적용됩니다."
                },
                if app.confirm_yes {
                    "  취소     [실행]"
                } else {
                    "[취소]      실행"
                }
            ),
        ),
    }
    let status = if app.busy.is_some() {
        "처리 중… Ctrl+C로 종료할 수 있습니다.".into()
    } else {
        app.notice.clone()
    };
    frame.render_widget(
        Paragraph::new(clean(&status))
            .wrap(Wrap { trim: false })
            .style(Style::default().fg(if app.notice.starts_with("오류:") {
                Color::LightRed
            } else {
                Color::Gray
            })),
        slots[2],
    );
    let keys = if app.searching {
        "검색 입력 · Enter/Esc 완료 · Ctrl+U 지우기"
    } else {
        match app.view {
            View::Chats => {
                "↑↓ 이동 · Enter 바로 재개 · → 상세/계정 · / 검색 · A 보관 · R 갱신 · Q 종료"
            }
            View::Detail => "↑↓ 이번 계정 선택 · Enter 적용 후 목록 · ←/Esc 목록 · F2 설정",
            View::Settings => "↑↓ 이동 · Enter 설정/변경 · ←/Esc 목록 · F2 목록",
            View::Accounts => {
                "↑↓ 계정 · A 추가 · L 로그인 · D 기본 · X/Delete 등록 해제 · U 사용량 · Esc 뒤로"
            }
            View::AuthMode => "↑↓ 인증 방식 · Enter 로그인 시작 · Esc 취소",
            View::Login => "O 브라우저 열기 · Esc 로그인 취소 · Ctrl+C 종료",
            View::Confirm => "←→ 선택 · Enter 확인 · Esc 취소",
            View::Nickname => "Enter 인증 방식 · Esc 취소",
        }
    };
    frame.render_widget(
        Paragraph::new(keys)
            .wrap(Wrap { trim: false })
            .style(Style::default().fg(BLUE)),
        slots[3],
    );
}
fn chats(frame: &mut Frame, area: Rect, app: &App) {
    let split = area.width >= 85;
    let panes =
        Layout::horizontal([Constraint::Percentage(55), Constraint::Percentage(45)]).split(area);
    if split || app.view == View::Chats {
        let rows = app
            .filtered
            .iter()
            .map(|i| {
                let chat = &app.snapshot.sessions[*i];
                ListItem::new(vec![
                    Line::from(format!(
                        "{}{}",
                        if chat.archived { "[보관] " } else { "" },
                        clean(&chat.title)
                    )),
                    Line::from(format!(
                        "  {} · {} · {}",
                        app.preferred(chat),
                        ago(chat.updated_at_ms),
                        clean(&chat.cwd)
                    ))
                    .style(Style::default().fg(Color::DarkGray)),
                ])
            })
            .collect();
        list(
            frame,
            if split { panes[0] } else { area },
            &format!(
                "채팅 {}개 · {} /{}{}",
                app.filtered.len(),
                if app.archived {
                    "보관 포함"
                } else {
                    "진행/저장"
                },
                clean(&app.query),
                if app.searching { "_" } else { "" }
            ),
            rows,
            app.selected,
            app.view == View::Chats,
        );
    }
    if split || app.view == View::Detail {
        let area = if split { panes[1] } else { area };
        if let Some(chat) = app.chat() {
            let parts =
                Layout::vertical([Constraint::Percentage(60), Constraint::Min(4)]).split(area);
            paragraph(
                frame,
                parts[0],
                "채팅 정보",
                format!(
                    "{}\n\nID: {}\n프로젝트: {}\n직전 계정: {}\n이번 실행: {}\n모델: {} · effort: {}\n\n모델·권한 등은 재개 후 공식 Codex CLI에서 변경합니다.",
                    clean(&chat.title),
                    clean(&chat.id),
                    clean(&chat.cwd),
                    if chat.account.is_empty() {
                        "기록 없음"
                    } else {
                        &chat.account
                    },
                    app.preferred(chat),
                    clean(&chat.model),
                    clean(&chat.effort)
                ),
            );
            let choices = app.account_names();
            let selected = if app.view == View::Detail {
                app.detail_account
            } else {
                choices
                    .iter()
                    .position(|n| *n == app.preferred(chat))
                    .unwrap_or(0)
            };
            list(
                frame,
                parts[1],
                "이번 실행 계정 · →로 변경",
                choices
                    .iter()
                    .map(|n| {
                        ListItem::new(if n == "default" {
                            "default · 기본 환경".into()
                        } else {
                            n.clone()
                        })
                    })
                    .collect(),
                selected,
                app.view == View::Detail,
            );
        } else {
            paragraph(
                frame,
                area,
                "채팅 정보",
                "선택할 채팅이 없습니다. 검색 조건이나 보관 목록을 확인하세요.".into(),
            );
        }
    }
}
fn settings(frame: &mut Frame, area: Rect, app: &App) {
    let parts = Layout::vertical([Constraint::Length(6), Constraint::Min(4)]).split(area);
    list(
        frame,
        parts[0],
        "전역 설정",
        vec![
            ListItem::new("계정 관리  →"),
            ListItem::new(format!(
                "기본 목록에 보관된 채팅 포함: {}",
                if app.preferences.include_archived {
                    "켜짐"
                } else {
                    "꺼짐"
                }
            )),
        ],
        app.setting_row,
        true,
    );
    paragraph(
        frame,
        parts[1],
        "현재 환경",
        format!(
            "대화 저장소: {}\n계정 저장소: {}\n설정 파일: {}\n\nTUI는 계정과 채팅 선택만 관리합니다.\nEnter로 넘긴 뒤에는 공식 Codex CLI가 작업을 이어갑니다.\n웹 서버·자동 계정 교체·history-sync는 필요하지 않습니다.",
            clean(app.snapshot.paths["history"].as_str().unwrap_or("")),
            clean(app.snapshot.paths["accounts"].as_str().unwrap_or("")),
            clean(app.snapshot.paths["config"].as_str().unwrap_or(""))
        ),
    );
}
fn remaining(value: &Value) -> String {
    value["usedPercent"]
        .as_i64()
        .map(|n| format!("{}% 남음", (100 - n).clamp(0, 100)))
        .unwrap_or("미확인".into())
}

fn ago(milliseconds: i64) -> String {
    if milliseconds <= 0 {
        return "시간 미확인".into();
    }
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as i64;
    let minutes = now.saturating_sub(milliseconds).max(0) / 60_000;
    if minutes < 1 {
        "방금".into()
    } else if minutes < 60 {
        format!("{minutes}분 전")
    } else if minutes < 1440 {
        format!("{}시간 전", minutes / 60)
    } else {
        format!("{}일 전", minutes / 1440)
    }
}
fn accounts(frame: &mut Frame, area: Rect, app: &App) {
    let parts = Layout::vertical([Constraint::Percentage(55), Constraint::Min(5)]).split(area);
    let rows = app
        .snapshot
        .accounts
        .iter()
        .map(|a| {
            ListItem::new(format!(
                "{}{}{}",
                a.name,
                if a.default { "  [기본]" } else { "" },
                if a.current { "  [현재 환경]" } else { "" }
            ))
        })
        .collect();
    list(
        frame,
        parts[0],
        "전체 계정 관리",
        rows,
        app.account_row,
        true,
    );
    let details = if let Some(account) = app.snapshot.accounts.get(app.account_row) {
        let usage = app.usage.get(&account.name);
        format!(
            "{}\n\n{}\n\n등록 해제는 인증·대화 파일을 삭제하지 않습니다.\n기본 계정 변경은 실행 중인 작업을 바꾸지 않습니다.",
            account.name,
            usage
                .map(|v| format!(
                    "플랜: {} · 5h {} · week {}\n{}",
                    clean(v["plan"].as_str().unwrap_or("")),
                    remaining(&v["rateLimits"]["primary"]),
                    remaining(&v["rateLimits"]["secondary"]),
                    clean(v["error"].as_str().unwrap_or(""))
                ))
                .unwrap_or("U 키로 계정/사용량 확인 · 자동으로 서버 조회하지 않습니다.".into())
        )
    } else {
        "등록된 계정이 없습니다. A 키로 계정을 추가하세요.\ndefault 환경의 인증은 그대로 사용할 수 있습니다.".into()
    };
    paragraph(frame, parts[1], "선택한 계정", details);
}
