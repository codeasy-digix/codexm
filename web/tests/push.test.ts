import assert from "node:assert/strict";
import { createECDH, randomBytes } from "node:crypto";
import { mkdtempSync, readFileSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { runInNewContext } from "node:vm";
import test from "node:test";
import { generateRequestDetails, type PushSubscription } from "web-push";
import { ChatPush, pushEndpoint, type ChatCompletion } from "../server/push";

const thread = "00000000-0000-4000-8000-000000000001";
const turn = "00000000-0000-4000-8000-000000000002";
const chrome = "https://fcm.googleapis.com/fcm/send/synthetic-endpoint";
const chatName = "배포 검증 채팅";
const title = `CodexM [${chatName}]`;

function subscription(endpoint = chrome): PushSubscription {
  const keys = createECDH("prime256v1");
  keys.generateKeys();
  return { endpoint, keys: { p256dh: keys.getPublicKey().toString("base64url"), auth: randomBytes(16).toString("base64url") } };
}

test("web push only accepts destinations for Chrome or Apple", () => {
  assert.equal(pushEndpoint(chrome), chrome);
  assert.equal(pushEndpoint("https://web.push.apple.com/synthetic-endpoint"), "https://web.push.apple.com/synthetic-endpoint");
  for (const endpoint of ["http://fcm.googleapis.com/fcm/send/x", "https://fcm.googleapis.com.bad.test/fcm/send/x",
    "https://fcm.googleapis.com:9200/fcm/send/x", "https://127.0.0.1/fcm/send/x", "https://fcm.googleapis.com@bad.test/fcm/send/x",
    "https://fcm.googleapis.com/", "https://web.push.apple.com/fake?destination=internal"]) {
    assert.equal(pushEndpoint(endpoint), null);
  }
});

test("background completion alerts use private stable keys, routing and durable deduplication", async t => {
  const dir = mkdtempSync(join(tmpdir(), "codexm-push-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  let now = Date.now(), scope = "fixture-credential";
  const completions: ChatCompletion[] = [];
  const received: { url: string; title: string; test?: boolean; tag?: string }[] = [];
  const make = () => new ChatPush(dir, "https://codex.example.invalid", () => scope, async () => completions,
    async (device, payload, options) => {
      const request = generateRequestDetails(device, payload, options);
      assert.equal(request.endpoint, chrome);
      assert.equal(request.headers["Content-Encoding"], "aes128gcm");
      assert.ok(request.headers.Authorization.startsWith("vapid"));
      assert.ok(Buffer.isBuffer(request.body) && !request.body.includes(payload));
      received.push(JSON.parse(payload));
    }, () => now);
  let service = make();
  const first = await service.action("browser", { action: "status" });
  assert.equal(statSync(join(dir, "push-vapid.json")).mode & 0o777, 0o600);
  assert.ok("publicKey" in first && first.publicKey);
  assert.ok(!JSON.stringify(first).includes("privateKey"));
  await service.action("browser", { action: "subscribe", subscription: subscription() });
  completions.push({ thread, turn: "00000000-0000-4000-8000-000000000003", at: now - 30_000 });
  now += 1_000;
  completions.push({ thread, turn, at: now, title: " 배포\n검증 채팅 " });
  await service.poll();
  assert.equal(received.length, 1);
  assert.equal(received[0].url, `/chat/${thread}`);
  assert.equal(received[0].title, title);
  assert.equal(received[0].tag?.length, 32);
  service = make();
  assert.equal((await service.action("another-session", { action: "status", endpoint: chrome })).publicKey, first.publicKey);
  await service.poll();
  assert.equal(received.length, 1, "restarting the web process must not replay completion alerts");
  await service.action("browser", { action: "test", endpoint: chrome, thread }, async id => {
    assert.equal(id, thread);
    return chatName;
  });
  assert.deepEqual(received[1], { url: `/chat/${thread}`, title, test: true });
  service.logout("renewed-session", chrome);
  assert.equal((await service.action("browser", { action: "status", endpoint: chrome })).subscribed, false);
  await service.action("new-device", { action: "subscribe", subscription: subscription() });
  scope = "changed-password";
  now += 1_000;
  completions.push({ thread, turn: "00000000-0000-4000-8000-000000000004", at: now });
  await service.poll();
  assert.equal(received.length, 2, "changing the credential revokes earlier devices");
});

test("installed worker displays an alert and opens the selected chat", async () => {
  const callbacks = new Map<string, (event: any) => void>();
  let notification: any, opened = "";
  const origin = "https://codex.example.invalid";
  const self = {
    location: { origin },
    addEventListener: (name: string, callback: (event: any) => void) => callbacks.set(name, callback),
    registration: { showNotification: async (title: string, data: any) => { notification = { title, ...data }; } },
    clients: { matchAll: async () => [], openWindow: async (url: string) => { opened = url; } },
  };
  runInNewContext(readFileSync(new URL("../public/sw.js", import.meta.url), "utf8"), { self, URL });
  const dispatch = async (name: string, event: object) => {
    let done: Promise<unknown> | undefined;
    callbacks.get(name)!({ ...event, waitUntil: (promise: Promise<unknown>) => { done = promise; } });
    await done;
  };
  await dispatch("push", { data: { json: () => ({ url: `/chat/${thread}`, title }) } });
  assert.equal(notification.title, title);
  assert.equal(notification.icon, "/icons/icon-192.png");
  assert.ok(!Object.hasOwn(notification, "body"));
  await dispatch("notificationclick", { notification: { ...notification, close() {} } });
  assert.equal(opened, `${origin}/chat/${thread}`);
  await dispatch("notificationclick", { notification: { data: { url: "//other.example.invalid" }, close() {} } });
  assert.equal(opened, `${origin}/`);
  await dispatch("push", { data: { json: () => ({ url: `/chat/${thread}` }) } });
  assert.equal(notification.title, "CodexM");
});
