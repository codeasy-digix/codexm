import React, { useEffect, useRef, useState } from "react";
import { call } from "../../client/data";

type Directory = { path: string; parent: string; directories: string[] };

export function FolderPicker({ value, onChange }: { value: string; onChange: (path: string) => void }) {
  const [open, setOpen] = useState(false);
  const [directory, setDirectory] = useState<Directory>();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const version = useRef(0);
  useEffect(() => () => { version.current++; }, []);

  async function browse(path: string) {
    const request = ++version.current;
    setError("");
    setPending(true);
    try {
      const opened = await call<Directory>("web.directories", { cwd: path });
      if (version.current === request) setDirectory(opened);
    } catch (reason) {
      if (version.current === request) setError((reason as Error).message);
    } finally {
      if (version.current === request) setPending(false);
    }
  }

  function close() {
    version.current++;
    setPending(false);
    setOpen(false);
  }

  return <div className="folder-picker">
    <div className="folder-field">
      <label>이 머신의 작업 폴더
        <input value={value} onChange={event => onChange(event.target.value)} placeholder="/absolute/project/path" required spellCheck={false} />
      </label>
      <button type="button" aria-expanded={open} onClick={() => {
        if (open) close();
        else {
          setDirectory(undefined);
          setOpen(true);
          void browse(value);
        }
      }}>폴더 찾기</button>
    </div>
    {open && <section className="folder-browser" aria-label="이 머신의 폴더">
      <div className="folder-navigation">
        <button type="button" disabled={!directory?.parent || pending} onClick={() => void browse(directory!.parent)}>↑ 상위 폴더</button>
        <button type="button" disabled={pending} onClick={() => void browse("")}>기본 폴더</button>
      </div>
      {directory && <p className="folder-path" title={directory.path}>{directory.path}</p>}
      {pending && <p role="status">폴더를 불러오는 중…</p>}
      {error && <p className="inline-error" role="alert">{error}</p>}
      {directory && <ul className="folder-entries">
        {directory.directories.map(name => <li key={name}><button type="button" disabled={pending}
          onClick={() => void browse(directory.path.replace(/\/$/, "") + "/" + name)}>{name}<span aria-hidden="true">›</span></button></li>)}
        {!directory.directories.length && <li className="muted">하위 폴더가 없습니다.</li>}
      </ul>}
      <div className="button-row"><button type="button" onClick={close}>닫기</button>
        <button className="primary" type="button" disabled={!directory || pending} onClick={() => {
          if (directory) onChange(directory.path);
          close();
        }}>이 폴더 선택</button></div>
    </section>}
  </div>;
}
