import { randomBytes, createHash, timingSafeEqual, pbkdf2 } from "node:crypto";
import { promisify } from "node:util";
import {
  readFileSync,
  writeFileSync,
  renameSync,
  mkdirSync,
  unlinkSync,
  statSync,
} from "node:fs";
import { join } from "node:path";

const digest = (text: string) =>
  createHash("sha256").update(text).digest("hex");
const secret = () => randomBytes(32).toString("base64url");
const derive = promisify(pbkdf2);
type Login = { username: string; algorithm: string; iterations: number; salt: string; hash: string };
type Session = { owner: string; expires: number; credential?: string };
export class LoginError extends Error {
  constructor(message: string, readonly status = 401) { super(message); }
}
export class LocalAuth {
  private sessions = new Map<string, Session>();
  private tickets = new Map<string, { session: string; expires: number }>();
  private attempts = new Map<string, { count: number; until: number }>();
  private inFlight = 0;
  readonly secure: boolean;
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
    this.secure = url.protocol === "https:";
    if (
      (!this.secure && (url.protocol !== "http:" || !["127.0.0.1", "localhost", "[::1]"].includes(url.hostname))) ||
      url.origin !== origin || url.pathname !== "/" || url.username || url.password || url.search || url.hash
    )
      throw new Error("an exact HTTPS or local HTTP origin is required");
    mkdirSync(dir, { recursive: true, mode: 0o700 });
    if (this.secure && !this.loginRecord()) throw new Error("public HTTPS requires a configured web login");
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
    if (!this.passwordEnabled()) this.rotateAccess();
  }
  private loginRecord(): Login | null {
    const filename = join(this.dir, "login.json");
    let text: string;
    try {
      const info = statSync(filename);
      if (!info.isFile() || (info.mode & 0o077) !== 0 || info.size > 4096) throw new Error("invalid web login file permissions");
      text = readFileSync(filename, "utf8");
    } catch (e) { if ((e as NodeJS.ErrnoException).code === "ENOENT") return null; throw e; }
    const row = JSON.parse(text) as Login;
    if (typeof row.username !== "string" || !/^[A-Za-z0-9_.@-]{1,64}$/.test(row.username) || row.algorithm !== "pbkdf2-sha256" || row.iterations !== 600000 || !/^[a-f0-9]{64}$/.test(row.salt) || !/^[a-f0-9]{64}$/.test(row.hash)) throw new Error("invalid web login configuration");
    return row;
  }
  passwordEnabled() { return !!this.loginRecord(); }
  private credential() {
    const row = this.loginRecord();
    if (this.secure && !row) throw new Error("web login is unavailable");
    return row ? digest(JSON.stringify(row)) : "";
  }
  private validSession(session: Session | undefined) {
    return !!session && session.expires > this.now() && (session.credential || "") === this.credential();
  }
  private throttle(remote: string) {
    const now = this.now();
    for (const [key, row] of this.attempts) if (row.until <= now) this.attempts.delete(key);
    if (this.attempts.size >= 2048) throw new LoginError("로그인 요청이 너무 많습니다. 잠시 후 다시 시도하세요.", 429);
    for (const [key, max] of [["global", 60], ["remote:" + remote, 10]] as const) {
      const row = this.attempts.get(key) || { count: 0, until: now + 60000 };
      row.count++; this.attempts.set(key, row);
      if (row.count > max) throw new LoginError("로그인 요청이 너무 많습니다. 잠시 후 다시 시도하세요.", 429);
    }
  }
  async login(username: unknown, password: unknown, remote: string) {
    this.throttle(remote);
    const row = this.loginRecord();
    const denied = () => new LoginError("아이디 또는 비밀번호가 올바르지 않습니다.");
    if (!row || typeof username !== "string" || typeof password !== "string" || username.length > 64 || Buffer.byteLength(password) > 1024) throw denied();
    if (this.inFlight >= 2) throw new LoginError("로그인 요청이 처리 중입니다. 잠시 후 다시 시도하세요.", 429);
    this.inFlight++;
    try {
      const hash = await derive(password, Buffer.from(row.salt, "hex"), row.iterations, 32, "sha256");
      if (!timingSafeEqual(hash, Buffer.from(row.hash, "hex")) || !timingSafeEqual(Buffer.from(digest(username)), Buffer.from(digest(row.username))) || digest(JSON.stringify(row)) !== this.credential()) throw denied();
      return this.createSession();
    } finally { this.inFlight--; }
  }
  cookie(token: string, expire = false) {
    return `codexm_session=${token}; HttpOnly; SameSite=Strict; Path=/; Max-Age=${expire ? 0 : 28800}${this.secure ? "; Secure" : ""}`;
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
    if (this.passwordEnabled() || this.secure) throw new LoginError("아이디와 비밀번호로 로그인하세요.");
    this.throttle(remote);
    // The local launcher can rotate this private file for a second browser.
    // Reading and consuming it are synchronous: only one exchange can win.
    let bootstrap = "";
    try {
      bootstrap = readFileSync(join(this.dir, "access"), "utf8").trim();
    } catch {}
    if (
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
    unlinkSync(join(this.dir, "access"));
    return this.createSession();
  }
  private createSession() {
    const now = this.now(), cookie = secret();
    for (const [key, s] of this.sessions)
      if (!this.validSession(s)) this.sessions.delete(key);
    if (this.sessions.size >= 64)
      throw new Error("브라우저 세션 한도를 초과했습니다.");
    this.sessions.set(digest(cookie), {
      owner: secret(),
      expires: now + 8 * 60 * 60 * 1000,
      credential: this.credential(),
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
    return this.validSession(s) ? key : null;
  }
  ticket(session: string) {
    if (!this.validSession(this.sessions.get(session))) throw new Error("인증이 필요합니다.");
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
    if (!row || row.expires <= this.now() || !this.validSession(this.sessions.get(row.session)))
      throw new Error("접속 인증이 만료되었습니다.");
    this.connections.set(connection, { session: row.session, close });
    return this.require(connection);
  }
  require(connection: string) {
    const row = this.connections.get(connection),
      session = row && this.sessions.get(row.session);
    if (!this.validSession(session))
      throw new Error("authentication_required");
    return session!.owner;
  }
  disconnect(connection: string) {
    this.connections.delete(connection);
  }
  logout(session: string) {
    const browser = this.sessions.get(session)?.owner;
    this.sessions.delete(session);
    this.save();
    for (const [id, c] of this.connections)
      if (c.session === session) {
        this.connections.delete(id);
        c.close();
      }
    return browser;
  }
}
