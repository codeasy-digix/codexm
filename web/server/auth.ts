import { randomBytes, createHash, timingSafeEqual } from "node:crypto";
import {
  readFileSync,
  writeFileSync,
  renameSync,
  mkdirSync,
  unlinkSync,
} from "node:fs";
import { join } from "node:path";

const digest = (text: string) =>
  createHash("sha256").update(text).digest("hex");
const secret = () => randomBytes(32).toString("base64url");
type Session = { owner: string; expires: number };
export class LocalAuth {
  private sessions = new Map<string, Session>();
  private tickets = new Map<string, { session: string; expires: number }>();
  private attempts = new Map<string, { count: number; until: number }>();
  readonly connections = new Map<
    string,
    { session: string; close: () => void }
  >();
  constructor(
    readonly dir: string,
    readonly origin: string,
    private now = Date.now,
  ) {
    const url = new URL(origin);
    if (
      url.protocol !== "http:" ||
      !["127.0.0.1", "localhost", "[::1]"].includes(url.hostname) ||
      url.pathname !== "/"
    )
      throw new Error("loopback origin required");
    mkdirSync(dir, { recursive: true, mode: 0o700 });
    try {
      const stored: [string, Session][] = JSON.parse(
        readFileSync(join(dir, "sessions.json"), "utf8"),
      );
      this.sessions = new Map(
        stored.filter(
          ([key, s]) =>
            /^[a-f0-9]{64}$/.test(key) &&
            s.expires > now() &&
            s.owner.length >= 16,
        ),
      );
    } catch {
      /* First launch or expired local sessions. */
    }
    this.rotateAccess();
  }
  private save() {
    const path = join(this.dir, "sessions.json");
    writeFileSync(path + ".next", JSON.stringify([...this.sessions]), {
      mode: 0o600,
    });
    renameSync(path + ".next", path);
  }
  rotateAccess() {
    const token = secret();
    const path = join(this.dir, "access");
    writeFileSync(path + ".next", token + "\n", { mode: 0o600 });
    renameSync(path + ".next", path);
  }
  validRequest(
    host: string | undefined,
    origin: string | undefined,
    requireOrigin = false,
  ) {
    return (
      host === new URL(this.origin).host &&
      (origin === this.origin || (!requireOrigin && origin === undefined))
    );
  }
  exchange(token: string, remote: string) {
    const now = this.now();
    for (const [key, row] of this.attempts)
      if (row.until <= now) this.attempts.delete(key);
    const attempt = this.attempts.get(remote) ?? {
      count: 0,
      until: now + 60_000,
    };
    attempt.count++;
    this.attempts.set(remote, attempt);
    // The local launcher can rotate this private file for a second browser.
    // Reading and consuming it are synchronous: only one exchange can win.
    let bootstrap = "";
    try {
      bootstrap = readFileSync(join(this.dir, "access"), "utf8").trim();
    } catch {}
    if (
      attempt.count > 10 ||
      !bootstrap ||
      token.length > 256 ||
      !timingSafeEqual(
        Buffer.from(digest(token)),
        Buffer.from(digest(bootstrap)),
      )
    )
      throw new Error(
        "접속 코드가 만료되었거나 올바르지 않습니다. codexm web을 다시 실행하세요.",
      );
    const cookie = secret();
    for (const [key, s] of this.sessions)
      if (s.expires <= now) this.sessions.delete(key);
    if (this.sessions.size >= 64)
      throw new Error("브라우저 세션 한도를 초과했습니다.");
    unlinkSync(join(this.dir, "access"));
    this.sessions.set(digest(cookie), {
      owner: secret(),
      expires: now + 8 * 60 * 60 * 1000,
    });
    this.save();
    return cookie;
  }
  session(cookieHeader: string | undefined) {
    const token = cookieHeader
      ?.split(";")
      .map((v) => v.trim())
      .find((v) => v.startsWith("codexm_session="))
      ?.slice(15);
    if (!token || token.length > 128) return null;
    const key = digest(token),
      s = this.sessions.get(key);
    return s && s.expires > this.now() ? key : null;
  }
  ticket(session: string) {
    if (!this.sessions.has(session)) throw new Error("인증이 필요합니다.");
    for (const [key, value] of this.tickets)
      if (value.expires <= this.now()) this.tickets.delete(key);
    if (this.tickets.size >= 128) throw new Error("접속 요청이 너무 많습니다.");
    const token = secret();
    this.tickets.set(digest(token), { session, expires: this.now() + 30_000 });
    return token;
  }
  authenticate(connection: string, ticket: string, close: () => void) {
    if (typeof ticket !== "string" || ticket.length > 128)
      throw new Error("인증이 필요합니다.");
    const key = digest(ticket),
      row = this.tickets.get(key);
    this.tickets.delete(key);
    if (!row || row.expires <= this.now() || !this.sessions.has(row.session))
      throw new Error("접속 인증이 만료되었습니다.");
    this.connections.set(connection, { session: row.session, close });
    return this.require(connection);
  }
  require(connection: string) {
    const row = this.connections.get(connection),
      session = row && this.sessions.get(row.session);
    if (!session || session.expires <= this.now())
      throw new Error("authentication_required");
    return session.owner;
  }
  disconnect(connection: string) {
    this.connections.delete(connection);
  }
  logout(session: string) {
    this.sessions.delete(session);
    this.save();
    for (const [id, c] of this.connections)
      if (c.session === session) {
        this.connections.delete(id);
        c.close();
      }
  }
}
