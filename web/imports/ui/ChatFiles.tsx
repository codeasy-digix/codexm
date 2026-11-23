import React, { useEffect, useMemo, useRef, useState } from "react";
import { call } from "../../client/data";
import { artifactPreview, artifactURL, type ChatArtifact, type ChatTurn } from "../contracts/web";
import { Modal } from "./Modal";

const emptyFiles: ChatArtifact[] = [];
const emptyRefs = new Map<string, string>();

export function useChatFiles(ready: boolean, online: boolean, thread: string, turns: ChatTurn[] | undefined, open: boolean) {
  const [seen, setSeen] = useState(() => new Map<string, ChatArtifact[]>());
  const [result, setResult] = useState<{ thread: string; error?: string }>();
  const attempt = useRef(0);
  const signature = useMemo(() => {
    for (let i = (turns?.length ?? 0) - 1; i >= 0; i--) {
      const turn = turns![i];
      if (turn.artifacts?.length || turn.items.some(item => item.type === "fileOutput")) {
        return `${turn.id}:${turn.artifacts?.length ?? 0}`;
      }
    }
  }, [turns]);
  const listed = seen.get(thread);
  const { files, refs } = useMemo(() => {
    if (!ready) return { files: emptyFiles, refs: emptyRefs };
    const byID = new Map<string, ChatArtifact>();
    const refs = new Map<string, string>();
    for (const turn of turns ?? []) {
      for (const file of turn.artifacts ?? []) byID.set(file.id, file);
      for (const [path, id] of Object.entries(turn.fileRefs ?? {})) refs.set(path, id);
    }
    for (const file of listed ?? []) byID.set(file.id, file);
    return { files: [...byID.values()], refs };
  }, [ready, turns, listed]);

  function save(id: string, files: ChatArtifact[]) {
    setSeen(previous => {
      if (JSON.stringify(previous.get(id)) === JSON.stringify(files)) return previous;
      const next = new Map(previous);
      next.delete(id);
      next.set(id, files);
      if (next.size > 12) next.delete(next.keys().next().value!);
      return next;
    });
  }

  useEffect(() => {
    if (!ready) { setSeen(new Map()); setResult(undefined); return; }
    if (!online || !thread || (!open && !signature)) return;
    const token = ++attempt.current;
    setResult(undefined);
    void call<ChatArtifact[]>("web.artifacts", { thread_id: thread }).then(files => {
      if (attempt.current !== token) return;
      save(thread, files);
      setResult({ thread });
    }).catch(error => {
      if (attempt.current === token) setResult({ thread, error: (error as Error).message });
    });
    return () => { attempt.current++; };
  }, [ready, online, thread, open, signature]);

  return {
    files, refs,
    loading: open && online && result?.thread !== thread,
    error: result?.thread === thread ? result.error : undefined,
    registered(files: ChatArtifact[]) {
      attempt.current++;
      save(thread, files);
      setResult({ thread });
    },
  };
}

export function FileLink({ thread, file, preview = false, label, alt }: {
  thread: string; file: ChatArtifact; preview?: boolean; label?: React.ReactNode; alt?: string;
}) {
  const href = artifactURL(thread, file.id);
  const kind = preview && artifactPreview(file.mime);
  const link = <a className="chat-file-link" href={href} download={file.name}>
    {kind === "image" && <img key={file.version} src={href} alt={alt || file.name} loading="lazy" />}
    <span>{label || file.name}</span>
  </a>;
  if (kind !== "video") return link;
  return <span className="chat-file-video">
    <video key={file.version} controls playsInline preload="metadata" src={href} aria-label={alt || file.name} />
    {link}
  </span>;
}

export function ChatFiles({ thread, files, loading, error: loadError, onRegistered, onClose }: {
  thread: string; files: ChatArtifact[]; loading: boolean; error?: string;
  onRegistered(files: ChatArtifact[]): void; onClose(): void;
}) {
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function register(event: React.FormEvent) {
    event.preventDefault();
    if (busy || !path.trim()) return;
    setBusy(true);
    setError("");
    try {
      const updated = await call<ChatArtifact[]>("web.artifactRegister", { thread_id: thread, file_path: path }, crypto.randomUUID());
      onRegistered(updated);
      setPath("");
    } catch (reason) { setError((reason as Error).message); }
    finally { setBusy(false); }
  }
  return <Modal title="채팅 파일" onClose={() => { if (!busy) onClose(); }}>
    {files.length ? <ul className="chat-files">
      {files.map(file => <li key={file.id}>
        <FileLink thread={thread} file={file} />
        <small>{file.source === "workspace" ? file.path : file.source === "attachment" ? "첨부 파일" : "Codex 생성"}</small>
      </li>)}
    </ul> : loading ? <p role="status">파일을 찾는 중…</p> : loadError ? null : <p className="muted">등록된 파일이 없습니다.</p>}
    {loadError && <p role="alert" className="inline-error">{loadError}</p>}
    <form className="dialog-form" onSubmit={event => void register(event)}>
      <label>작업 폴더 기준 파일 경로
        <input value={path} onChange={event => setPath(event.target.value)} placeholder="예: output/report.html" maxLength={4096} required />
      </label>
      {error && <p role="alert" className="inline-error">{error}</p>}
      <div className="button-row">
        <button type="button" disabled={busy} onClick={onClose}>닫기</button>
        <button className="primary" disabled={busy || !path.trim()}>{busy ? "등록 중…" : "파일 등록"}</button>
      </div>
    </form>
  </Modal>;
}
