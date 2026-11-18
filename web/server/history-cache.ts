import { createHash } from "node:crypto";
import { lstatSync, realpathSync } from "node:fs";
import { join } from "node:path";
import { MongoClient, type Collection } from "mongodb";
import { chatIdFromPath, type ChatHistory, type ChatHistoryPage } from "../imports/contracts/web";

type PageKey = { id: string; thread: string };
type CachedPage = {
  _id: string;
  thread: string;
  revision: string;
  page: ChatHistoryPage;
  expiresAt: Date;
};
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
  private readonly directory: string;
  private client?: MongoClient;
  private collection?: Collection<CachedPage>;
  private indexes?: Promise<unknown>;
  private retryAfter = 0;

  constructor(state: string) {
    this.directory = join(state, "history-cache");
  }

  private pages() {
    if (Date.now() < this.retryAfter) return;
    if (this.collection) return this.collection;
    let socket: string;
    try {
      const path = realpathSync(this.directory);
      const directory = lstatSync(path);
      socket = join(path, "mongodb.sock");
      const endpoint = lstatSync(socket);
      const uid = process.getuid?.();
      if (!directory.isDirectory() || !endpoint.isSocket() || directory.uid !== uid ||
        endpoint.uid !== uid || (directory.mode & 0o077) !== 0) return;
    } catch { return; }
    this.client = new MongoClient("mongodb://" + encodeURIComponent(socket) + "/codexm", {
      directConnection: true, serverSelectionTimeoutMS: 250, connectTimeoutMS: 250,
      socketTimeoutMS: 500, maxPoolSize: 4, waitQueueTimeoutMS: 250, retryReads: false, retryWrites: false,
    });
    return this.collection = this.client.db().collection<CachedPage>("history_pages_v1");
  }

  private async load(key?: PageKey) {
    if (!key) return;
    try {
      return await this.pages()?.findOne({ _id: key.id, expiresAt: { $gt: new Date() } });
    } catch {
      this.retryAfter = Date.now() + 5000;
    }
  }

  async peek(params: Record<string, unknown>, nativeVersion: (thread: string) => Promise<string>) {
    const key = pageKey(params);
    const saved = await this.load(key);
    if (!saved || !key) return null;
    const revision = await nativeVersion(key.thread);
    if (!revision) await this.deleted(key.thread);
    return revision === saved.revision ? saved.page : null;
  }

  async read(params: Record<string, unknown>, nativeRead: (revision: string) => Promise<NativeHistory>) {
    const key = pageKey(params);
    const saved = await this.load(key);
    const result = await nativeRead(saved?.revision ?? "");
    const { historyRevision: revision, historyUnchanged, ...response } = result;
    if (historyUnchanged && saved && revision === saved.revision) {
      return { ...response, ...saved.page };
    }
    if (key && revision && Array.isArray(response.turns) && response.turns.length) {
      await this.save(key, revision, { turns: response.turns, nextCursor: response.nextCursor ?? null });
    }
    return response;
  }

  private async save(key: PageKey, revision: string, page: ChatHistoryPage) {
    if (!/^[0-9a-f]{64}$/.test(revision) || Buffer.byteLength(JSON.stringify(page)) > 4 * 1024 * 1024) return;
    try {
      const pages = this.pages();
      if (!pages) return;
      this.indexes ??= Promise.all([
        pages.createIndex({ expiresAt: 1 }, { expireAfterSeconds: 0 }),
        pages.createIndex({ thread: 1 }),
      ]);
      await this.indexes;
      await pages.replaceOne({ _id: key.id }, {
        thread: key.thread, revision, page, expiresAt: new Date(Date.now() + 30 * 86_400_000),
      }, { upsert: true });
    } catch {
      this.indexes = undefined;
      this.retryAfter = Date.now() + 5000;
    }
  }

  async deleted(thread: string) {
    try {
      await this.pages()?.deleteMany({ thread });
    } catch {
      this.retryAfter = Date.now() + 5000;
    }
  }

  async close() { await this.client?.close(); }
}
