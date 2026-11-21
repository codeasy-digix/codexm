import { createHash, ECDH, randomUUID } from "node:crypto";
import { lstatSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { generateVAPIDKeys, sendNotification, type PushSubscription, type RequestOptions, type VapidKeys } from "web-push";
import { LoginError } from "./auth";

export type ChatCompletion = { thread: string; turn: string; at: number; title?: string };
type PushMessage = { url: string; title: string; test?: boolean; tag?: string };
type Device = { subscription: PushSubscription; scope: string; owner: string; since: number };
type State = { devices: Record<string, Device>; sent: Record<string, number> };
type Deliver = (subscription: PushSubscription, payload: string, options: RequestOptions) => Promise<unknown>;

const uuid = /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i;
// Cover a short restart while allowing for the native read's five-minute limit.
const lookback = 4 * 60_000 + 30_000;
const hash = (value: string) => createHash("sha256").update(value).digest("hex");

function notificationTitle(name?: string) {
  const title = typeof name === "string" ? name.replace(/\s+/g, " ").trim() : "";
  return title ? `CodexM [${Array.from(title).slice(0, 256).join("")}]` : "CodexM";
}

// Only browser push providers are destinations. Neither an authenticated
// caller nor a locally restored subscription can turn the server into a proxy.
export function pushEndpoint(value: unknown): string | null {
  if (typeof value !== "string" || value.length > 2048) return null;
  try {
    const url = new URL(value);
    if (url.protocol !== "https:" || url.port || url.username || url.password || url.hash || url.search) return null;
    if (!/^\/[A-Za-z0-9_./=:%-]+$/.test(url.pathname)) return null;
    const chrome = url.hostname === "fcm.googleapis.com" && /^\/(?:fcm\/send|wp)\/.+/.test(url.pathname);
    const apple = /^(?:[a-z0-9-]+\.)+push\.apple\.com$/.test(url.hostname);
    return chrome || apple ? url.href : null;
  } catch { return null; }
}

function validKey(value: unknown, size: number): value is string {
  if (typeof value !== "string" || value.length > 88 || !/^[A-Za-z0-9_-]+={0,2}$/.test(value)) return false;
  const decoded = Buffer.from(value, "base64url");
  return decoded.length === size && decoded.toString("base64url") === value.replace(/=+$/, "");
}

function readSubscription(value: unknown): PushSubscription | null {
  if (!value || typeof value !== "object") return null;
  const { endpoint, keys } = value as Partial<PushSubscription>;
  const destination = pushEndpoint(endpoint);
  if (!destination || !validKey(keys?.p256dh, 65) || !validKey(keys?.auth, 16)) return null;
  try { ECDH.convertKey(keys.p256dh, "prime256v1", "base64url"); }
  catch { return null; }
  return { endpoint: destination, keys: { p256dh: keys.p256dh, auth: keys.auth } };
}

function privateJSON<T>(filename: string, missing: () => T): T {
  try {
    const stat = lstatSync(filename);
    if (!stat.isFile() || (stat.mode & 0o077) || stat.size > 256 * 1024) throw new Error("invalid push state file");
    return JSON.parse(readFileSync(filename, "utf8"));
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return missing();
    throw error;
  }
}

function savePrivate(filename: string, value: unknown) {
  const staged = `${filename}.${randomUUID()}.next`;
  try {
    writeFileSync(staged, JSON.stringify(value), { mode: 0o600, flag: "wx", flush: true });
    renameSync(staged, filename);
  } finally { rmSync(staged, { force: true }); }
}

export class ChatPush {
  private readonly stateFile: string;
  private readonly keys: VapidKeys;
  private state: State;
  private polling = false;
  private tests = new Map<string, number>();

  constructor(
    dir: string,
    private readonly subject: string,
    private readonly currentScope: () => string,
    private readonly completions: (after: number) => Promise<ChatCompletion[]>,
    private readonly deliver: Deliver = sendNotification,
    private readonly now = Date.now,
  ) {
    const keyFile = join(dir, "push-vapid.json");
    this.keys = privateJSON(keyFile, () => {
      const keys = generateVAPIDKeys();
      savePrivate(keyFile, keys);
      return keys;
    });
    if (!validKey(this.keys?.publicKey, 65) || !validKey(this.keys?.privateKey, 32)) throw new Error("invalid VAPID key pair");
    this.stateFile = join(dir, "push-devices.json");
    this.state = privateJSON<State>(this.stateFile, () => ({ devices: {}, sent: {} }));
    if (!this.state.devices || !this.state.sent) throw new Error("invalid push subscription state");
  }

  private commit(state: State) {
    savePrivate(this.stateFile, state);
    this.state = state;
  }

  async action(owner: string, value: unknown, lookupTitle?: (thread: string) => Promise<string | undefined>) {
    if (!value || typeof value !== "object") throw new LoginError("알림 요청을 확인해 주세요.", 400);
    const input = value as { action?: unknown; endpoint?: unknown; subscription?: unknown; thread?: unknown };
    const scope = this.currentScope();
    const endpoint = pushEndpoint(input.endpoint);
    const id = endpoint ? hash(endpoint) : "";
    const device = this.state.devices[id];
    if (input.action === "status") return { publicKey: this.keys.publicKey, subscribed: device?.scope === scope };
    if (input.action === "subscribe") {
      const subscription = readSubscription(input.subscription);
      if (!subscription) throw new LoginError("Chrome에서 이 기기의 알림 구독을 확인해 주세요.", 400);
      const key = hash(subscription.endpoint);
      const prior = this.state.devices[key];
      const devices = Object.fromEntries(Object.entries(this.state.devices).filter(([, item]) => item.scope === scope));
      if (!prior && Object.keys(devices).length >= 32) throw new LoginError("알림 기기 등록 한도에 도달했습니다.", 429);
      devices[key] = { subscription, scope, owner, since: prior?.scope === scope ? prior.since : this.now() };
      this.commit({ ...this.state, devices });
      return { subscribed: true };
    }
    if (input.action === "unsubscribe") {
      if (device?.scope === scope) this.remove(id, device);
      return { subscribed: false };
    }
    if (input.action === "test") {
      if (!device || device.scope !== scope) throw new LoginError("이 기기에서 알림을 먼저 켜 주세요.", 400);
      const now = this.now();
      if ((this.tests.get(id) ?? 0) > now - 10_000) throw new LoginError("잠시 뒤에 다시 시험해 주세요.", 429);
      this.tests.set(id, now);
      const thread = typeof input.thread === "string" && uuid.test(input.thread) ? input.thread.toLowerCase() : "";
      const title = thread && lookupTitle ? await lookupTitle(thread).catch(() => undefined) : undefined;
      if (!await this.send(id, device, { url: thread ? `/chat/${thread}` : "/", title: notificationTitle(title || "테스트 알림"), test: true })) {
        throw new LoginError("알림을 보내지 못했습니다. 구독 상태를 확인해 주세요.", 503);
      }
      return { accepted: true };
    }
    throw new LoginError("지원하지 않는 알림 요청입니다.", 400);
  }

  logout(owner: string | undefined, endpoint: unknown) {
    const address = pushEndpoint(endpoint);
    const id = address ? hash(address) : "";
    const devices = Object.fromEntries(Object.entries(this.state.devices).filter(([key, device]) => key !== id && device.owner !== owner));
    if (Object.keys(devices).length !== Object.keys(this.state.devices).length) this.commit({ ...this.state, devices });
  }

  private remove(id: string, device: Device) {
    if (this.state.devices[id] !== device) return;
    const devices = { ...this.state.devices };
    delete devices[id];
    this.tests.delete(id);
    this.commit({ ...this.state, devices });
  }

  private async send(id: string, device: Device, payload: PushMessage) {
    const subscription = readSubscription(device.subscription);
    if (!subscription || this.state.devices[id] !== device || this.currentScope() !== device.scope) return false;
    try {
      await this.deliver(subscription, JSON.stringify(payload), {
        vapidDetails: { subject: this.subject, ...this.keys },
        contentEncoding: "aes128gcm", TTL: payload.test ? 120 : 86_400, timeout: 8000,
        urgency: "normal", topic: payload.tag,
      });
      return true;
    } catch (error) {
      const status = (error as {statusCode?: number}).statusCode;
      if (status === 404 || status === 410) this.remove(id, device);
      return false;
    }
  }

  async poll() {
    if (this.polling || !Object.keys(this.state.devices).length) return;
    this.polling = true;
    try {
      const scope = this.currentScope();
      if (!Object.values(this.state.devices).some(device => device.scope === scope)) {
        this.commit({ ...this.state, devices: {} });
        return;
      }
      const now = this.now();
      const after = now - lookback;
      const completions = await this.completions(after);
      const sent = Object.fromEntries(Object.entries(this.state.sent).filter(([, at]) => at >= after));
      const deliveries: Array<{ tag: string; chat: ChatCompletion }> = [];
      for (const chat of completions) {
        if (!uuid.test(chat.thread) || !uuid.test(chat.turn) || !Number.isSafeInteger(chat.at) || chat.at < after || chat.at > this.now()) continue;
        const tag = hash(`${chat.thread.toLowerCase()}/${chat.turn.toLowerCase()}`).slice(0, 32);
        if (sent[tag] || !Object.values(this.state.devices).some(device => device.scope === scope && device.since <= chat.at)) continue;
        sent[tag] = chat.at;
        deliveries.push({ tag, chat });
      }
      if (!deliveries.length) return;
      // Checkpoint before transport. An interrupted request has an uncertain
      // outcome, so a restarted worker never sends it again.
      this.commit({ ...this.state, sent });
      for (const { tag, chat } of deliveries) {
        const devices = Object.entries(this.state.devices).filter(([, device]) => device.scope === scope && device.since <= chat.at);
        const payload = { url: `/chat/${chat.thread.toLowerCase()}`, title: notificationTitle(chat.title), tag };
        await Promise.allSettled(devices.map(([id, device]) => this.send(id, device, payload)));
      }
    } finally { this.polling = false; }
  }
}
