import { randomBytes } from "node:crypto";
import { spawn } from "node:child_process";
import { accessSync, constants } from "node:fs";
import { realpath, stat } from "node:fs/promises";
import type { IncomingMessage } from "node:http";
import type { Duplex } from "node:stream";
import { homedir, userInfo } from "node:os";
import { isAbsolute, relative, sep } from "node:path";
import { WebSocket, WebSocketServer } from "ws";
import { chatIdFromPath, type ChatThread, type Project } from "../imports/contracts/web";
import { LoginError, type LocalAuth } from "./auth";

const socketPath = "/terminal/ws";
const frameLimit = 64 << 10;
type Grant = { session: string; cwd: string; expires: number };
type Client = { session: string; alive: boolean };

async function directory(path: string | undefined) {
  if (!path || !isAbsolute(path)) return;
  try {
    const resolved = await realpath(path);
    if ((await stat(resolved)).isDirectory()) return resolved;
  } catch { /* A saved project directory may have been moved. */ }
}

function dimensions(payload: { cols?: unknown; rows?: unknown }) {
  const { cols, rows } = payload;
  return {
    cols: typeof cols === "number" && Number.isInteger(cols) && cols >= 2 && cols <= 500 ? cols : 80,
    rows: typeof rows === "number" && Number.isInteger(rows) && rows >= 2 && rows <= 250 ? rows : 24,
  };
}

export class LocalTerminal {
  private grants = new Map<string, Grant>();
  private clients = new Map<WebSocket, Client>();
  private server = new WebSocketServer({ noServer: true, maxPayload: frameLimit, perMessageDeflate: false });
  readonly available: boolean;

  constructor(private helper: string, private auth: LocalAuth, private lookup: (id: string) => Promise<ChatThread | null>,
    private projects: () => Promise<Project[]>) {
    this.available = this.checkHelper();
    if (this.available) setInterval(() => this.checkConnections(), 20_000).unref();
  }

  private checkHelper() {
    if (!isAbsolute(this.helper)) return false;
    try { accessSync(this.helper, constants.X_OK); return true; }
    catch { return false; }
  }

  async ticket(cookie: string | undefined, input: { thread?: unknown; project?: unknown }) {
    const session = this.auth.session(cookie);
    if (!session) throw new LoginError("로그인이 필요합니다.");
    if (!this.available) throw new LoginError("이 웹 설치에 터미널 실행 파일이 없습니다.", 503);
    if (this.atCapacity(session)) throw new LoginError("열려 있는 터미널을 하나 종료한 뒤 다시 연결해 주세요.", 429);
    const thread = input?.thread ?? "", project = input?.project ?? "";
    if (typeof thread !== "string" || thread && chatIdFromPath(`/chat/${thread}`) !== thread ||
        typeof project !== "string" || project.length > 256) throw new LoginError("작업 폴더를 확인할 수 없습니다.", 400);
    let cwd;
    if (thread) {
      const chat = await this.lookup(thread);
      if (!chat || chat.id !== thread) throw new LoginError("채팅을 찾을 수 없습니다.", 404);
      if (input.project !== "" && chat.projectId) cwd = await directory(chat.cwd);
      else if (project) {
        // A new draft can predate its persisted project metadata. Use the
        // project chosen in the browser only for a directory inside that root.
        const selected = (await this.projects()).find(row => row.id === project);
        const working = await directory(chat.cwd);
        for (const root of selected?.roots ?? []) {
          const base = await directory(root.path);
          if (!working || !base) continue;
          const inside = relative(base, working);
          if (!isAbsolute(inside) && inside !== ".." && !inside.startsWith(`..${sep}`)) {
            cwd = working;
            break;
          }
        }
      }
    } else if (project) {
      const selected = (await this.projects()).find(row => row.id === project);
      cwd = await directory(selected?.roots[0]?.path);
    }
    cwd ??= await directory(homedir()) ?? homedir();
    if (this.auth.session(cookie) !== session) throw new LoginError("로그인이 필요합니다.");
    this.pruneGrants();
    if (this.grants.size >= 64) throw new LoginError("터미널 연결 요청이 너무 많습니다.", 429);
    const ticket = randomBytes(32).toString("base64url");
    this.grants.set(ticket, { session, cwd, expires: Date.now() + 15_000 });
    return { ticket, path: socketPath };
  }

  upgrade(req: IncomingMessage, socket: Duplex, head: Buffer) {
    if (req.url !== socketPath) return;
    let session: string | null = null;
    try { session = this.auth.session(req.headers.cookie); }
    catch { /* Leave the socket unauthenticated if web login is unavailable. */ }
    if (!this.available || !session || this.atCapacity(session)) {
      const status = !session ? "401 Unauthorized" : this.available ? "429 Too Many Requests" : "503 Service Unavailable";
      socket.end(`HTTP/1.1 ${status}\r\nConnection: close\r\nContent-Length: 0\r\n\r\n`);
      return;
    }
    this.server.handleUpgrade(req, socket, head, ws => this.connect(ws, session, req.headers.cookie));
  }

  closeBrowser(session: string) {
    for (const [ticket, grant] of this.grants) if (grant.session === session) this.grants.delete(ticket);
    for (const [socket, client] of this.clients) if (client.session === session) socket.terminate();
  }

  private atCapacity(session: string) {
    return this.clients.size >= 16 || [...this.clients.values()].filter(client => client.session === session).length >= 4;
  }

  private pruneGrants() {
    const now = Date.now();
    for (const [ticket, grant] of this.grants) if (grant.expires <= now) this.grants.delete(ticket);
  }

  private checkConnections() {
    this.pruneGrants();
    for (const [socket, client] of this.clients) {
      if (!client.alive) { socket.terminate(); continue; }
      client.alive = false;
      socket.ping();
    }
  }

  private connect(socket: WebSocket, session: string, cookie: string | undefined) {
    const client = { session, alive: true };
    this.clients.set(socket, client);
    let terminal: ReturnType<LocalTerminal["start"]> | undefined;
    const deadline = setTimeout(() => socket.terminate(), 8000);
    const authenticated = () => {
      try { return this.auth.session(cookie) === session; }
      catch { return false; }
    };
    const send = (message: object) => {
      if (socket.readyState !== WebSocket.OPEN) return;
      if (socket.bufferedAmount >= 1024 * 1024) { socket.terminate(); return; }
      socket.send(JSON.stringify(message));
    };
    socket.on("pong", () => {
      if (authenticated()) client.alive = true;
      else socket.terminate();
    });
    socket.on("message", (frame, binary) => {
      if (binary || !authenticated()) { socket.terminate(); return; }
      try {
        const message = JSON.parse(frame.toString());
        if (!terminal) {
          const grant = typeof message.ticket === "string" && this.grants.get(message.ticket);
          if (message.type !== "auth" || !grant || grant.session !== session || grant.expires <= Date.now()) {
            socket.close(1008, "Invalid terminal ticket");
            return;
          }
          this.grants.delete(message.ticket);
          terminal = this.start(grant.cwd, dimensions(message), send, () => socket.close(1000));
          clearTimeout(deadline);
        } else if (terminal.ready() && message.type === "input" && typeof message.data === "string" && Buffer.byteLength(message.data) <= 32 << 10) {
          terminal.send({ type: "write", data: message.data });
        } else if (terminal.ready() && message.type === "resize") {
          terminal.send({ type: "resize", ...dimensions(message) });
        }
      } catch { socket.close(1008, "Invalid terminal frame"); }
    });
    socket.on("error", () => socket.terminate());
    socket.once("close", () => {
      clearTimeout(deadline);
      this.clients.delete(socket);
      terminal?.stop();
    });
  }

  // The helper is the web_terminal Rust PTY runtime. Its stdin carries JSONL
  // controls, stdout carries terminal text, and stderr carries bounded status.
  private start(cwd: string, size: ReturnType<typeof dimensions>, output: (value: object) => void, close: () => void) {
    const user = userInfo();
    const shell = user.shell && isAbsolute(user.shell) ? user.shell : "/bin/sh";
    const env: NodeJS.ProcessEnv = { HOME: homedir(), USER: user.username, LOGNAME: user.username, SHELL: shell,
      TERM: "xterm-256color", COLORTERM: "truecolor", LANG: process.env.LANG || "en_US.UTF-8" };
    for (const [key, value] of Object.entries(process.env)) {
      if (/^(PATH|TMPDIR|XDG_(CONFIG|DATA|STATE|CACHE)_HOME|LC_[A-Z_]+)$/.test(key)) env[key] = value;
    }
    const child = spawn(this.helper, [], { env, stdio: ["pipe", "pipe", "pipe"] });
    let ready = false, stopped = false, status = "";
    let exitCode: number | undefined;
    const stop = () => {
      if (stopped) return;
      stopped = true;
      child.stdin.end();
      child.kill("SIGTERM");
    };
    const failure = () => {
      output({ type: "error", message: "이 머신에서 터미널을 시작하거나 유지하지 못했습니다." });
      stop();
      close();
    };
    const deadline = setTimeout(failure, 8000);
    deadline.unref();
    const send = (message: object) => {
      if (stopped || child.stdin.destroyed) return;
      const line = JSON.stringify(message) + "\n";
      if (child.stdin.writableLength + Buffer.byteLength(line) > 1024 * 1024) { failure(); return; }
      child.stdin.write(line);
    };
    child.stdin.on("error", () => { if (!stopped) failure(); });
    child.on("error", failure);
    child.stdout.setEncoding("utf8").on("data", data => output({ type: "output", data }));
    child.stderr.setEncoding("utf8").on("data", data => {
      status += data;
      if (status.length > 16 * 1024) { failure(); return; }
      const lines = status.split("\n");
      status = lines.pop() ?? "";
      for (const line of lines) {
        try {
          const message = JSON.parse(line);
          if (message.type === "ready" && message.protocol === 1 && !ready) {
            ready = true;
            clearTimeout(deadline);
            output({ type: "ready", cwd });
          } else if (message.type === "exit" && Number.isInteger(message.exitCode)) exitCode = message.exitCode;
          else if (message.type === "error") failure();
        } catch { failure(); }
      }
    });
    child.once("close", () => {
      clearTimeout(deadline);
      if (!stopped && exitCode !== undefined) output({ type: "exit", exitCode });
      stopped = true;
      close();
    });
    send({ type: "spawn", protocol: 1, file: shell, args: ["-l"], cwd, ...size });
    return { ready: () => ready, send, stop };
  }
}
