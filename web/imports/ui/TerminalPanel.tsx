import React, { useEffect, useEffectEvent, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

export type TerminalLocation = { thread: string; project: string };

export default function TerminalPanel({ visible, location, onHide, onEnd }: {
  visible: boolean;
  location: TerminalLocation;
  onHide: () => void;
  onEnd: (restoreFocus: boolean) => void;
}) {
  const dock = useRef<HTMLElement>(null);
  const container = useRef<HTMLDivElement>(null);
  const active = useRef<Terminal | undefined>(undefined);
  const isVisible = useRef(visible);
  isVisible.current = visible;
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState<"connecting" | "ready" | "closed">("connecting");
  const [cwd, setCwd] = useState("");
  const [error, setError] = useState("");
  const shellExited = useEffectEvent(() => onEnd(dock.current?.contains(document.activeElement) ?? false));
  useEffect(() => {
    if (visible) active.current?.focus();
  }, [visible]);

  useEffect(() => {
    const element = container.current!;
    const terminal = new Terminal({
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace', fontSize: 13,
      cursorBlink: true, scrollback: 4000, theme: { background: "#18181c", foreground: "#ececef" },
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(element);
    terminal.textarea?.setAttribute("aria-label", "터미널 입력");
    active.current = terminal;
    const controller = new AbortController();
    let socket: WebSocket | undefined;
    let ready = false;
    let pendingOutput = 0;
    const send = (message: object) => {
      if (socket?.readyState !== WebSocket.OPEN) return;
      if (socket.bufferedAmount >= 256 * 1024) { socket.close(1008); return; }
      socket.send(JSON.stringify(message));
    };
    const resize = () => {
      if (!element.clientWidth || !element.clientHeight) return;
      fit.fit();
      if (ready) send({ type: "resize", cols: terminal.cols, rows: terminal.rows });
    };
    const observer = new ResizeObserver(resize);
    observer.observe(element);
    const input = terminal.onData(data => {
      if (!ready) return;
      for (let offset = 0; offset < data.length;) {
        let end = Math.min(offset + 4096, data.length);
        if (end < data.length && /[\uD800-\uDBFF]/.test(data[end - 1])) end--;
        send({ type: "input", data: data.slice(offset, end) });
        offset = end;
      }
    });
    setStatus("connecting");
    setError("");
    setCwd("");
    void (async () => {
      const response = await fetch("/auth/terminal-ticket", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(location),
        signal: AbortSignal.any([controller.signal, AbortSignal.timeout(12_000)]),
      });
      const grant = await response.json();
      if (!response.ok) throw new Error(grant.error || "터미널에 연결할 수 없습니다.");
      if (controller.signal.aborted) return;
      const address = new URL(grant.path, window.location.origin);
      address.protocol = address.protocol === "https:" ? "wss:" : "ws:";
      socket = new WebSocket(address);
      socket.onopen = () => {
        resize();
        send({ type: "auth", ticket: grant.ticket, cols: terminal.cols, rows: terminal.rows });
      };
      socket.onmessage = event => {
        if (controller.signal.aborted) return;
        const message = JSON.parse(event.data);
        if (message.type === "output") {
          pendingOutput += message.data.length;
          if (pendingOutput > 1024 * 1024) { socket?.close(1008); return; }
          terminal.write(message.data, () => { pendingOutput -= message.data.length; });
        } else if (message.type === "ready") {
          ready = true;
          setStatus("ready");
          setCwd(message.cwd);
          resize();
          if (isVisible.current) terminal.focus();
        } else if (message.type === "exit") {
          ready = false;
          shellExited();
        } else if (message.type === "error") setError(message.message);
      };
      socket.onerror = () => { if (!controller.signal.aborted) setError("터미널 연결에 실패했습니다."); };
      socket.onclose = () => {
        ready = false;
        if (!controller.signal.aborted) setStatus("closed");
      };
    })().catch(reason => {
      if (controller.signal.aborted) return;
      setError((reason as Error).message);
      setStatus("closed");
    });
    return () => {
      controller.abort();
      socket?.close();
      observer.disconnect();
      input.dispose();
      active.current = undefined;
      terminal.dispose();
    };
  }, [location, attempt]);

  function key(value: string) {
    const terminal = active.current;
    if (!terminal) return;
    const arrows: Record<string, string> = { "←": "D", "↑": "A", "↓": "B", "→": "C" };
    const sequence = arrows[value] ? (terminal.modes.applicationCursorKeysMode ? "\x1bO" : "\x1b[") + arrows[value]
      : value === "Ctrl+C" ? "\x03" : value === "Tab" ? "\t" : "\x1b";
    terminal.input(sequence);
    terminal.focus();
  }

  return <section ref={dock} className="terminal-dock" aria-label="터미널" hidden={!visible}>
    <header className="terminal-toolbar">
      <strong>터미널</strong>
      <small title={cwd} role="status">{status === "connecting" ? "연결 중…" : status === "closed" ? "연결 종료" : cwd}</small>
      {status === "closed" && <button type="button" onClick={() => setAttempt(value => value + 1)}>다시 연결</button>}
      <button type="button" aria-label="터미널 종료" title="터미널 종료" onClick={() => onEnd(true)}>종료</button>
      <button type="button" aria-label="터미널 접기" title="터미널 접기" onClick={onHide}>▾</button>
    </header>
    {error && <p className="terminal-error" role="alert">{error}</p>}
    <div className="terminal-viewport" ref={container} />
    <div className="terminal-keys" aria-label="터미널 특수 키">
      {["Esc", "Tab", "←", "↑", "↓", "→", "Ctrl+C"].map(value => <button type="button" key={value}
        disabled={status !== "ready"} onPointerDown={event => event.preventDefault()} onClick={() => key(value)}>{value}</button>)}
    </div>
  </section>;
}
