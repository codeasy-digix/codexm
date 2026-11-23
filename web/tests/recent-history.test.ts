import assert from "node:assert/strict";
import { test } from "node:test";
import type { ChatThread, ChatTurn } from "../imports/contracts/web";
import { RecentHistory, transcriptBasis, updateTranscript } from "../imports/ui/recent-history";
import { ChatHistoryCache } from "../server/history-cache";
import type { CacheDatabase } from "../server/cache-database";

const thread: ChatThread = {
  id: "00000000-0000-4000-8000-000000000061", cwd: "/fixture", status: { type: "idle" }, updatedAt: 1,
};
const metadata = { thread, queue: [], pending: [] };
const turn = (id: string, text = id): ChatTurn => ({
  id, status: "completed", items: [{ id, type: "agentMessage", text }],
});

test("a warm transcript receives only changed turns and retains earlier pages", async () => {
  const cache = new ChatHistoryCache({ get: () => undefined } as unknown as CacheDatabase);
  const revision = "a".repeat(64);
  const old = await cache.read({ thread_id: thread.id, history_basis: { turns: [] } }, async () =>
    ({ ...metadata, turns: [turn("b"), turn("c")], nextCursor: "before-b", historyRevision: revision }));
  const previous = updateTranscript(undefined, old)!;
  const older = { ...previous, turns: [turn("a"), ...previous.turns], nextCursor: "before-a" };
  const params = { thread_id: thread.id, history_basis: transcriptBasis(older) };
  const same = await cache.read(params, async known => {
    assert.equal(known, revision);
    return { ...metadata, historyRevision: revision, historyUnchanged: true };
  });
  assert.equal(same.turns, undefined);
  assert.equal(same.relayedInputs, undefined);
  assert.equal(updateTranscript(older, same)?.turns, older.turns);

  const changed = await cache.read(params, async () => ({
    ...metadata, turns: [turn("b"), turn("c", "updated"), turn("d")], nextCursor: "before-b", historyRevision: "b".repeat(64),
  }));
  assert.deepEqual(changed.turns?.map(t => t.id), ["c", "d"]);
  assert.equal(updateTranscript(undefined, changed), undefined);
  const result = updateTranscript(older, changed)!;
  assert.deepEqual(result.turns.map(t => t.id), ["a", "b", "c", "d"]);
  assert.equal(result.turns[1], older.turns[1]);
  assert.equal(result.turns[2].items[0].text, "updated");
  assert.equal(result.nextCursor, "before-a");
});

test("a latest window beyond the cached transcript uses its new cursor", () => {
  const older = { turns: [turn("a")], nextCursor: "before-a" };
  const next = updateTranscript(older, { ...metadata, turns: [turn("z")], nextCursor: "before-z" });
  assert.deepEqual(next?.turns.map(t => t.id), ["z"]);
  assert.equal(next?.nextCursor, "before-z");
});

test("revisited transcripts are retained within the browser memory cap", () => {
  const memory = new RecentHistory();
  const page = { turns: [turn("a")] };
  for (let i = 0; i < 12; i++) memory.retain(String(i), page);
  memory.retain("0", memory.peek("0")!);
  memory.retain("12", page);
  assert.equal(memory.peek("1"), undefined);
  assert.equal(memory.peek("0"), page);
  memory.clear();
  assert.equal(memory.peek("0"), undefined);
});
