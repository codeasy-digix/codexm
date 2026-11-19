import assert from "node:assert/strict";
import { test } from "node:test";
import { CacheDatabase } from "../server/cache-database";
import { ChatCatalogCache } from "../server/catalog-cache";

const ids = [1, 2, 3].map(n => `00000000-0000-4000-8000-00000000000${n}`);
const meta = { accounts: [], projects: [], defaultCwd: "/workspace" };
const rows = ids.map((id, index) => ({
  at: 40 - index * 10, position: 1 - index,
  thread: { id, cwd: "/workspace", updatedAt: 40 - index * 10, status: { type: "notLoaded" },
    pinned: index < 2, archived: index === 2 },
}));

test("matching cached pages stay available and consistent during an incomplete refresh", async () => {
  const requests: string[] = [];
  let nextPage: (() => void) | undefined;
  let revision = "one";
  const cache = new ChatCatalogCache(new CacheDatabase("/nonexistent-codexm-fixture"), async cursor => {
    requests.push(cursor);
    if (!cursor) return { revision, meta, rows: rows.slice(0, 1), nextCursor: revision + ".1" };
    if (revision === "two") {
      await new Promise<void>(resolve => { nextPage = resolve; });
      return { revision: "changed", rows: rows.slice(1) };
    }
    return { revision, rows: rows.slice(1) };
  });
  const first = await cache.read({ limit: 1 });
  assert.deepEqual(requests, ["", "one.1"]);
  assert.deepEqual(first.threads.map(chat => chat.id), [ids[0]]);
  assert.deepEqual(first.pinned.map(chat => chat.id), [ids[1], ids[0]]);
  assert.deepEqual((await cache.read({ limit: 1, cursor: first.nextCursor })).threads.map(chat => chat.id), [ids[1]]);
  assert.deepEqual((await cache.read({ archived: true })).threads.map(chat => chat.id), [ids[2]]);

  revision = "two";
  const refresh = cache.refresh(true);
  const expected = assert.rejects(refresh, /갱신됐습니다/);
  assert.equal(cache.refresh(), refresh);
  assert.deepEqual(await cache.read({ limit: 1 }), first);
  assert.equal(requests.length, 4);
  nextPage!();
  await expected;
  assert.deepEqual(await cache.read({ limit: 1 }), first);
});
