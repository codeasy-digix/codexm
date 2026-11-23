import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { LocalAuth } from "../server/auth.ts";
import {
  directCommand,
  remaining,
  shouldSend,
  sessionKey,
} from "../imports/contracts/types.ts";
test("one-time local access, origin checks, ticket replay, owner continuity and expiry", () => {
  const dir = mkdtempSync(join(tmpdir(), "codexm-auth-"));
  let now = 1000;
  try {
    const auth = new LocalAuth(dir, "http://127.0.0.1:4317", () => now);
    assert.equal(
      auth.validRequest("127.0.0.1:4317", "https://evil.example", true),
      false,
    );
    assert.equal(
      auth.validRequest("evil.example:4317", "http://127.0.0.1:4317"),
      false,
    );
    assert.equal(auth.validRequest("127.0.0.1:4317", undefined, true), false);
    const code = readFileSync(join(dir, "access"), "utf8").trim(),
      cookie = auth.exchange(code, "local");
    assert.throws(() => auth.exchange(code, "local"));
    auth.rotateAccess();
    const secondCode = readFileSync(join(dir, "access"), "utf8").trim();
    assert.notEqual(code, secondCode);
    assert.throws(() => auth.exchange(code, "local"));
    assert.ok(auth.exchange(secondCode, "local"));
    const session = auth.session("codexm_session=" + cookie)!;
    assert.ok(session);
    assert.throws(() => auth.require("unauthenticated"));
    const ticket = auth.ticket(session),
      first = auth.authenticate("tab1", ticket, () => {});
    assert.throws(() => auth.authenticate("tab2", ticket, () => {}));
    auth.disconnect("tab1");
    assert.equal(
      auth.authenticate("reconnected", auth.ticket(session), () => {}),
      first,
    );
    const restored = new LocalAuth(dir, "http://127.0.0.1:4317", () => now);
    assert.equal(
      restored.authenticate("reload", restored.ticket(session), () => {}),
      first,
    );
    const late = auth.ticket(session);
    now += 31_000;
    assert.throws(() => auth.authenticate("late", late, () => {}));
    assert.equal(auth.logout(session), first);
    assert.throws(() => auth.require("reconnected"));
    assert.equal(new LocalAuth(dir, "http://127.0.0.1:4317", () => now).session("codexm_session=" + cookie), null);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
test("a valid legacy cookie renews across restarts with the same browser owner", () => {
  const dir = mkdtempSync(join(tmpdir(), "codexm-resume-"));
  const origin = "http://127.0.0.1:4317", day = 24 * 3600000;
  let now = 1000;
  try {
    const auth = new LocalAuth(dir, origin, () => now);
    const token = auth.exchange(readFileSync(join(dir, "access"), "utf8").trim(), "fixture");
    const cookie = "codexm_session=" + token;
    const session = auth.session(cookie)!;
    const owner = auth.authenticate("initial", auth.ticket(session), () => {});
    const filename = join(dir, "sessions.json");
    const stored = JSON.parse(readFileSync(filename, "utf8"));
    stored[0][1].expires = now + 8 * 3600000;
    writeFileSync(filename, JSON.stringify(stored));

    now += 7 * 3600000;
    const updated = new LocalAuth(dir, origin, () => now);
    const renewed = updated.resume(cookie);
    assert.equal(renewed.session, session);
    assert.match(renewed.cookie!, /Max-Age=7776000$/);
    assert.equal(updated.resume(cookie).cookie, undefined);
    assert.ok(!readFileSync(filename, "utf8").includes(token));

    now += 89 * day;
    const reopened = new LocalAuth(dir, origin, () => now);
    assert.equal(reopened.authenticate("reopened", reopened.ticket(reopened.resume(cookie).session), () => {}), owner);
    now += 2 * day;
    assert.equal(reopened.session(cookie), session);
    now += 89 * day;
    assert.throws(() => reopened.resume(cookie), { status: 401 });
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
test("Korean IME, multiline input, direct commands, unknown quota and composite identity", () => {
  const enter = {
    key: "Enter",
    shiftKey: false,
    isComposing: false,
    keyCode: 13,
  };
  assert.equal(shouldSend(enter), true);
  assert.equal(shouldSend({ ...enter, isComposing: true }), false);
  assert.equal(shouldSend({ ...enter, keyCode: 229 }), false);
  assert.equal(shouldSend({ ...enter, shiftKey: true }), false);
  assert.equal(directCommand(" /accounts ")?.view, "accounts");
  assert.equal(directCommand("/accounts tell me something"), undefined);
  assert.equal(remaining(null), "미확인");
  assert.equal(remaining({ usedPercent: 100 }), "0%");
  assert.notEqual(sessionKey("a", "store"), sessionKey("b", "store"));
});
