import { test } from "node:test";
import assert from "node:assert/strict";
import { pbkdf2Sync } from "node:crypto";
import { mkdtempSync, readFileSync, writeFileSync, rmSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { LocalAuth } from "../server/auth.ts";

function credentials(dir: string, password = "fixture-password") {
  const salt = "ab".repeat(32);
  writeFileSync(join(dir, "login.json"), JSON.stringify({ username: "fixture", algorithm: "pbkdf2-sha256", iterations: 600000, salt,
    hash: pbkdf2Sync(password, Buffer.from(salt, "hex"), 600000, 32, "sha256").toString("hex") }), { mode: 0o600 });
}
test("HTTPS password login gates tickets; rejects code bypass and revokes existing sessions on rotation", async () => {
  const dir = mkdtempSync(join(tmpdir(), "codexm-password-"));
  try {
    assert.throws(() => new LocalAuth(dir, "http://203.0.113.20"));
    assert.throws(() => new LocalAuth(dir, "https://203.0.113.20"));
    credentials(dir);
    const auth = new LocalAuth(dir, "https://203.0.113.20");
    assert.equal(auth.passwordEnabled(), true);
    assert.equal(auth.validRequest("203.0.113.20", "https://evil.example", true), false);
    assert.equal(auth.validRequest("203.0.113.20", undefined, true), false);
    assert.equal(auth.validRequest("203.0.113.20", "https://203.0.113.20", true), true);
    assert.throws(() => auth.ticket("missing"));
    assert.throws(() => auth.exchange("anything", "client"));
    await assert.rejects(auth.login("fixture", "wrong-password", "client"));
    await assert.rejects(auth.login("wrong-user", "fixture-password", "client"));
    const token = await auth.login("fixture", "fixture-password", "client");
    assert.match(auth.cookie(token), /; Secure$/);
    assert.match(auth.cookie(token), /HttpOnly; SameSite=Strict/);
    const session = auth.session("codexm_session=" + token)!;
    assert.ok(session);
    const ticket = auth.ticket(session), owner = auth.authenticate("browser", ticket, () => {});
    assert.ok(owner);
    assert.throws(() => auth.authenticate("replay", ticket, () => {}));
    const restored = new LocalAuth(dir, "https://203.0.113.20");
    assert.equal(restored.session("codexm_session=" + token), session);
    const pending = auth.ticket(session);
    credentials(dir, "different-password");
    assert.equal(auth.session("codexm_session=" + token), null);
    assert.throws(() => auth.require("browser"));
    assert.throws(() => auth.authenticate("new", pending, () => {}));
    const stored = readFileSync(join(dir, "sessions.json"), "utf8");
    assert.ok(!stored.includes(token) && !stored.includes("fixture-password"));
    chmodSync(join(dir, "login.json"), 0o644);
    assert.throws(() => new LocalAuth(dir, "https://203.0.113.20"));
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
test("password login has bounded per-client attempts and sessions expire/logout", async () => {
  const dir = mkdtempSync(join(tmpdir(), "codexm-throttle-"));
  let now = 1;
  try {
    credentials(dir);
    const auth = new LocalAuth(dir, "https://localhost", () => now);
    for (let i = 0; i < 10; i++) await assert.rejects(auth.login(null, null, "client"));
    await assert.rejects(auth.login("fixture", "fixture-password", "client"), (e: any) => e.status === 429);
    now += 60001;
    const token = await auth.login("fixture", "fixture-password", "client");
    const session = auth.session("codexm_session=" + token)!;
    let closed = false;
    auth.authenticate("browser", auth.ticket(session), () => { closed = true; });
    auth.logout(session);
    assert.equal(closed, true);
    assert.equal(auth.session("codexm_session=" + token), null);
    const expiring = await auth.login("fixture", "fixture-password", "client");
    now += 8 * 3600000;
    assert.equal(auth.session("codexm_session=" + expiring), null);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
