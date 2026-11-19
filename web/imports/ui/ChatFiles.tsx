import React, { useEffect, useMemo, useRef, useState } from "react";
import { call } from "../../client/data";
import { artifactURL, inlineArtifact, type ChatArtifact, type ChatTurn } from "../contracts/web";
import { Modal } from "./Modal";

const emptyFiles: ChatArtifact[] = [];

export function useChatFiles(ready: boolean, online: boolean, thread: string, turns: ChatTurn[] | undefined, open: boolean) {
  const [seen, setSeen] = useState(() => new Map<string, ChatArtifact[]>());
  const [result, setResult] = useState<{ thread: string; error?: string }>();
  const attempt = useRef(0);
  const signature = useMemo(() => {
    if (!turns) return;
    for (let i = turns.length - 1; i >= 0; i--) {
      const turn = turns[i];
      const refs = turn.items.flatMap(item => {
        if (item.type === "agentMessage" && !item.streaming)
          return item.text?.match(/visualize[^]*|\]\([^)]+\)/g) ?? [];
        if (item.type === "userMessage")
          return item.content?.filter(part => part.type === "localImage").map(part => String(part.path ?? "")) ?? [];
        return [];
      });
      if (refs.length) return JSON.stringify([turn.id, refs]);
    }
  }, [turns]);

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
    if (!ready) { setSeen(new Map()); setResult(undefined); }
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
    files: ready ? seen.get(thread) ?? emptyFiles : emptyFiles,
    loading: open && online && result?.thread !== thread,
    error: result?.thread === thread ? result.error : undefined,
    registered(files: ChatArtifact[]) {
      attempt.current++;
      save(thread, files);
      setResult({ thread });
    },
  };
}

export function FileLink({ thread, file, preview = false }: { thread: string; file: ChatArtifact; preview?: boolean }) {
  const href = artifactURL(thread, file.id);
  return <a className="chat-file-link" href={href} download={file.name}>
    {preview && inlineArtifact(file.mime) && <img key={file.version} src={href} alt={file.name} loading="lazy" />}
    <span>{file.name}</span>
  </a>;
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
    {loading ? <p role="status">파일을 찾는 중…</p> : files.length ? <ul className="chat-files">
      {files.map(file => <li key={file.id}>
        <FileLink thread={thread} file={file} />
        <small>{file.source === "visualization" ? "Codex 생성" : file.path}</small>
      </li>)}
    </ul> : loadError ? null : <p className="muted">등록된 파일이 없습니다.</p>}
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
