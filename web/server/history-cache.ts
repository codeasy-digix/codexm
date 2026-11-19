import { createHash } from "node:crypto";
import type { Collection } from "mongodb";
import { chatIdFromPath, type ChatHistory, type ChatHistoryPage } from "../imports/contracts/web";
import type { CacheDatabase } from "./cache-database";

type PageKey = { id: string; thread: string };
type CachedPage = {
  _id: string;
  thread: string;
  revision: string;
  page: ChatHistoryPage;
  format?: number;
  expiresAt?: Date;
};
type HistorySource = { thread: string; revision: string };
type SavedProgress = { _id: string; revision: string; nextCursor: string | null };
type NativePage = ChatHistoryPage & { historyRevision: string };
type NativeHistory = Omit<ChatHistory, keyof ChatHistoryPage> & Partial<ChatHistoryPage> & {
  historyRevision?: string;
  historyUnchanged?: boolean;
};

function pageKey(params: Record<string, unknown>): PageKey | undefined {
  const { thread_id: thread } = params;
  const cursor = params.cursor ?? "";
  const limit = params.limit === 0 ? 8 : params.limit ?? 8;
  if (typeof thread !== "string" || chatIdFromPath("/chat/" + thread) !== thread ||
    typeof cursor !== "string" || Buffer.byteLength(cursor) > 4096 ||
    typeof limit !== "number" || !Number.isInteger(limit) || limit < 1 || limit > 20) return;
  const id = createHash("sha256").update(JSON.stringify([thread, cursor, limit])).digest("hex");
  return { id, thread };
}

export class ChatHistoryCache {
  private collection?: Collection<CachedPage>;
  private progress?: Collection<SavedProgress>;
  private indexes?: Promise<unknown>;
  private readonly threadWrites = new Map<string, Promise<unknown>>();
  private retryAfter = 0;
  private scanning = false;
  private lastThread = "";

  constructor(private readonly database: CacheDatabase) {}

  private pages() {
    if (Date.now() < this.retryAfter) return;
    if (this.collection) return this.collection;
    const db = this.database.get();
    if (!db) return;
    this.progress = db.collection<SavedProgress>("history_progress_v1");
    return this.collection = db.collection<CachedPage>("history_pages_v1");
  }

  private async prepare(pages: Collection<CachedPage>) {
    try { await pages.dropIndex("expiresAt_1"); }
    catch (error) {
      if (![26, 27].includes((error as { code?: number }).code ?? 0)) throw error;
    }
    await pages.createIndex({ thread: 1 });
    await pages.updateMany({ expiresAt: { $exists: true } }, { $unset: { expiresAt: "" } });
  }

  private async load(key?: PageKey) {
    if (!key) return;
    try {
      return await this.pages()?.findOne({ _id: key.id, format: 2 });
    } catch {
      this.retryAfter = Date.now() + 5000;
    }
  }

  // A foreground read, a background page and an explicit deletion for the same
  // thread must finish in order. An old write cannot invalidate a newer cursor.
  private async withThread<T>(thread: string, action: () => Promise<T>) {
    const current = (this.threadWrites.get(thread) ?? Promise.resolve()).catch(() => {}).then(action);
    this.threadWrites.set(thread, current);
    try { return await current; }
    finally {
      if (this.threadWrites.get(thread) === current) this.threadWrites.delete(thread);
    }
  }

  async peek(params: Record<string, unknown>, nativeVersion: (thread: string) => Promise<string>) {
    const key = pageKey(params);
    const saved = await this.load(key);
    if (!saved || !key) return null;
    const revision = await nativeVersion(key.thread);
    return revision === saved.revision ? saved.page : null;
  }

  async read(params: Record<string, unknown>, nativeRead: (revision: string) => Promise<NativeHistory>) {
    const key = pageKey(params);
    if (!key || !this.pages()) return nativeRead("");
    return this.withThread(key.thread, async () => {
      const saved = await this.load(key);
      const result = await nativeRead(saved?.revision ?? "");
      const { historyRevision: revision, historyUnchanged, ...response } = result;
      if (historyUnchanged && saved && revision === saved.revision) {
        return { ...response, ...saved.page };
      }
      if (revision && Array.isArray(response.turns) && response.turns.length) {
        await this.save(key, revision, { turns: response.turns, nextCursor: response.nextCursor ?? null });
      }
      return response;
    });
  }

  private async save(key: PageKey, revision: string, page: ChatHistoryPage) {
    if (!/^[0-9a-f]{64}$/.test(revision) || Buffer.byteLength(JSON.stringify(page)) > 12 * 1024 * 1024) return false;
    try {
      const pages = this.pages();
      if (!pages) return false;
      this.indexes ??= this.prepare(pages);
      await this.indexes;
      const receipt = await pages.replaceOne({ _id: key.id }, {
        thread: key.thread, revision, page, format: 2,
      }, { upsert: true, writeConcern: { w: 1, journal: true } });
      return receipt.acknowledged;
    } catch {
      this.indexes = undefined;
      this.retryAfter = Date.now() + 5000;
      return false;
    }
  }

  // One in-flight walk and one checkpoint per thread. A cursor is published
  // only after its page is durable, so restart or temporary errors can repeat
  // that read without losing turns. The old native cursors are revision-bound.
  async refresh(
    sources: () => Promise<HistorySource[]>,
    nativePage: (source: HistorySource, cursor: string) => Promise<NativePage | null>,
    busy: () => boolean,
  ) {
    if (this.scanning || busy()) return;
    const pages = this.pages(), progress = this.progress;
    if (!pages || !progress) return;
    this.scanning = true;
    try {
      this.indexes ??= this.prepare(pages);
      await this.indexes;
      if (busy()) return;
      const current = await sources();
      const saved = new Map((await progress.find({ _id: { $in: current.map(s => s.thread) } }).toArray()).map(row => [row._id, row]));
      const offset = current.findIndex(s => s.thread === this.lastThread) + 1;
      let budget = 4;
      for (const source of [...current.slice(offset), ...current.slice(0, offset)]) {
        if (busy() || budget === 0) break;
        this.lastThread = source.thread;
        const previous = saved.get(source.thread);
        const cursor = previous?.revision === source.revision ? previous.nextCursor : "";
        if (cursor === null) continue;
        const key = pageKey({ thread_id: source.thread, cursor });
        if (!key) continue;
        budget--;
        await this.withThread(source.thread, async () => {
          const cached = await this.load(key);
          let page = cached?.revision === source.revision ? cached.page : undefined;
          if (!page) {
            if (busy()) return;
            const fetched = await nativePage(source, cursor).catch(() => null);
            if (!fetched || fetched.historyRevision !== source.revision || !Array.isArray(fetched.turns)) return;
            page = { turns: fetched.turns, nextCursor: fetched.nextCursor ?? null };
            if (!await this.save(key, source.revision, page)) return;
          }
          const nextCursor = page.nextCursor ?? null;
          if (nextCursor !== null && (!nextCursor || nextCursor === cursor || !page.turns.length)) return;
          await progress.updateOne({ _id: source.thread }, { $set: { revision: source.revision, nextCursor } },
            { upsert: true, writeConcern: { w: 1, journal: true } });
        });
      }
    } catch {
      this.indexes = undefined;
      this.retryAfter = Date.now() + 5000;
    } finally {
      this.scanning = false;
    }
  }

  async deleted(thread: string) {
    await this.withThread(thread, async () => {
      try {
        await this.pages()?.deleteMany({ thread });
        await this.progress?.deleteOne({ _id: thread });
      } catch {
        this.retryAfter = Date.now() + 5000;
      }
    });
  }

}
