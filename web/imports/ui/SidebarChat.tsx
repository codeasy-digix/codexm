import React from "react";
import { chatPath, chatTitle, type ChatActivity, type ChatThread } from "../contracts/web";
import { ChatActivityLabel } from "./ChatActivity";

function Icon({ kind }: { kind: "pin" | "archive" | "restore" | "rename" | "fork" }) {
  return <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor"
    strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {kind === "pin" && <path d="M16 3l5 5-3 1-3 4 .3 3-1.5 1.5-3.7-3.7L5 19l4.8-5.2-3.6-3.6 1.4-1.5 3 .3 4.4-3 .9-3Z" />}
    {kind === "archive" && <path d="M4 8V5h16v3H4zm1 0v11h14V8m-10 4h6" />}
    {kind === "restore" && <path d="M4 8V5h16v3H4zm1 0v11h14V8m-7 8v-5m-2 2 2-2 2 2" />}
    {kind === "rename" && <path d="m4 16-.8 4.8L8 20l11-11-4-4L4 16zm9.6-9.6 4 4M15 5l2-2a1.4 1.4 0 0 1 2 0l2 2a1.4 1.4 0 0 1 0 2l-2 2" />}
    {kind === "fork" && <><circle cx="6" cy="5" r="2"/><circle cx="18" cy="5" r="2"/><circle cx="6" cy="19" r="2"/>
      <path d="M6 7v10m12-10v1a5 5 0 0 1-5 5h-3a4 4 0 0 0-4 4"/></>}
  </svg>;
}

type Props = {
  chat: ChatThread;
  selected: boolean;
  pinnedTab: boolean;
  busy: boolean;
  known: boolean;
  activity?: ChatActivity;
  onOpen: () => void;
  onPin: () => void;
  onArchive: () => void;
  onCommand: (command: "rename" | "fork") => void;
};

export function SidebarChat({ chat, selected, pinnedTab, busy, known, activity, onOpen, onPin, onArchive, onCommand }: Props) {
  const name = chatTitle(chat);
  const canToggle = !chat.archived && (pinnedTab || !chat.pinned);
  const pinLabel = pinnedTab ? "고정 해제" : "고정하기";
  const archiveLabel = chat.archived ? "보관 해제" : "보관하기";
  return <div className={`sidebar-chat-row${selected ? " is-current" : ""}`} role="group" aria-label={name}>
    <a href={chatPath(chat.id)} aria-current={selected ? "page" : undefined} title={name} onClick={event => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      onOpen();
    }}>
      <strong>{name}</strong>
      <span className="chat-row-meta"><small>{chat.cwd.split("/").filter(Boolean).pop() || chat.cwd}</small>
        {!chat.archived && <ChatActivityLabel activity={activity} known={known}/>}
      </span>
    </a>
    <div className="sidebar-chat-actions">
      {canToggle && <button type="button" aria-label={pinLabel} title={pinLabel} aria-pressed={Boolean(chat.pinned)}
        disabled={busy} onClick={onPin}><Icon kind="pin" /></button>}
      {!canToggle && chat.pinned && !chat.archived &&
        <span className="pin-mark" aria-label="고정됨" title="고정 탭에서 해제"><Icon kind="pin" /></span>}
      <button type="button" aria-label="이름 변경" title="이름 변경" onClick={() => onCommand("rename")}><Icon kind="rename" /></button>
      <button className="row-archive" type="button" aria-label={archiveLabel} title={archiveLabel} onClick={onArchive}><Icon kind={chat.archived ? "restore" : "archive"} /></button>
      <button className="row-fork" type="button" aria-label="채팅 복제 (Fork)" title="채팅 복제 (Fork)" onClick={() => onCommand("fork")}><Icon kind="fork" /></button>
    </div>
  </div>;
}
