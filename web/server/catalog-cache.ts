import { createHash } from "node:crypto";
import { chatIdFromPath, type Catalog, type ChatThread } from "../imports/contracts/web";
import type { CacheDatabase } from "./cache-database";

type Metadata = Omit<Catalog, "threads" | "pinned" | "nextCursor" | "total">;
type Row = { thread: ChatThread; at: number; position: number; nativeProject?: string };
type IndexPage = { revision: string; rows: Row[]; meta?: Metadata; nextCursor?: string };
type Manifest = { _id: "current"; generation: string; meta: Metadata; pages: number; digest: string };
type Chunk = { _id: string; generation: string; index: number; rows: Row[] };
type Index = { meta: Metadata; rows: Row[]; pinned: ChatThread[]; digest: string };

function assemble(meta: Metadata, chunks: Row[][], digest: string): Index {
  const rows = chunks.flat();
  const pinned = rows.filter(({ thread }) => thread.pinned && !thread.archived)
    .sort((a, b) => a.position - b.position || a.thread.id.localeCompare(b.thread.id))
    .map(({ thread }) => thread);
  return { meta, rows, pinned, digest };
}

function queryString(params: Record<string, unknown>, key: string, max: number) {
  const value = params[key] ?? "";
  if (typeof value !== "string" || Buffer.byteLength(value) > max) throw new Error("잘못된 채팅 목록 조회 조건입니다.");
  return value;
}

export function catalogPage(index: Index, params: Record<string, unknown>): Catalog {
  const project = queryString(params, "project_id", 256);
  const cwd = queryString(params, "cwd", 4096);
  const search = queryString(params, "search", 500).toLowerCase().trim();
  const cursor = queryString(params, "cursor", 4096);
  const archived = params.archived ?? false;
  const limit = params.limit === 0 ? 50 : params.limit ?? 50;
  if (typeof archived !== "boolean" || typeof limit !== "number" || !Number.isInteger(limit) || limit < 1 || limit > 100) {
    throw new Error("잘못된 채팅 목록 조회 조건입니다.");
  }
  const query = createHash("sha256").update(JSON.stringify([project, cwd, search, archived])).digest("hex");
  let after: { q: string; at: number; id: string } | undefined;
  if (cursor) {
    try {
      after = JSON.parse(Buffer.from(cursor, "base64url").toString("utf8"));
      if (!after || after.q !== query || !Number.isSafeInteger(after.at) ||
        typeof after.id !== "string" || chatIdFromPath("/chat/" + after.id) !== after.id) throw new Error();
    } catch { throw new Error("목록 위치가 바뀌었습니다. 목록을 다시 불러와 주세요."); }
  }
  const matching = index.rows.filter(({ thread, nativeProject }) =>
    Boolean(thread.archived) === archived && (!project || project === thread.projectId || project === nativeProject) &&
    (!cwd || cwd === thread.cwd) && (!search || `${thread.name ?? ""} ${thread.preview ?? ""}`.toLowerCase().includes(search)));
  const offset = after ? matching.findIndex(row => row.at < after.at || (row.at === after.at && row.thread.id > after.id)) : 0;
  const page = offset < 0 ? [] : matching.slice(offset, offset + limit);
  const last = page.at(-1);
  const nextCursor = last && offset + page.length < matching.length
    ? Buffer.from(JSON.stringify({ q: query, at: last.at, id: last.thread.id })).toString("base64url") : null;
  return { ...index.meta, threads: page.map(({ thread }) => thread), pinned: index.pinned, total: matching.length, nextCursor };
}

// The manifest becomes visible after every chunk has been journaled. Failed
// refreshes keep the last full index available, including during a Mongo outage.
export class ChatCatalogCache {
  private index?: Index;
  private hydration?: Promise<void>;
  private inFlight?: Promise<void>;
  private followUp?: Promise<void>;
  private writes = Promise.resolve();
  private savedDigest = "";
  private attemptedAt = 0;

  constructor(private readonly database: CacheDatabase, private readonly nativePage: (cursor: string) => Promise<IndexPage>) {}

  private hydrate() {
    return this.hydration ??= (async () => {
      const db = this.database.get();
      if (!db) return;
      const manifest = await db.collection<Manifest>("catalog_manifest_v1").findOne({ _id: "current" });
      if (!manifest) return;
      const chunks = await db.collection<Chunk>("catalog_pages_v1").find({ generation: manifest.generation }).sort({ index: 1 }).toArray();
      if (chunks.length !== manifest.pages || chunks.some((chunk, i) => chunk.index !== i)) return;
      this.savedDigest = manifest.digest;
      this.index = assemble(manifest.meta, chunks.map(chunk => chunk.rows), manifest.digest);
    })().catch(() => {});
  }

  async read(params: Record<string, unknown>) {
    await this.hydrate();
    if (!this.index) await this.refresh();
    if (!this.index) throw new Error("채팅 목록을 불러올 수 없습니다.");
    return catalogPage(this.index, params);
  }

  refresh(force = false): Promise<void> {
    if (this.inFlight) {
      if (!force) return this.inFlight;
      return this.followUp ??= this.inFlight.catch(() => {}).then(() => {
        this.followUp = undefined;
        return this.refresh(true);
      });
    }
    if (!force && this.index && Date.now() - this.attemptedAt < 12_000) return Promise.resolve();
    this.attemptedAt = Date.now();
    this.inFlight = this.capture().finally(() => { this.inFlight = undefined; });
    return this.inFlight;
  }

  private async capture() {
    await this.hydrate();
    let page = await this.nativePage("");
    const { meta, revision } = page;
    if (!meta || !revision || !Array.isArray(page.rows)) throw new Error("채팅 목록 원본을 확인할 수 없습니다.");
    const chunks = [page.rows];
    const cursors = new Set<string>();
    while (page.nextCursor) {
      if (cursors.has(page.nextCursor)) throw new Error("채팅 목록 원본 페이지가 멈췄습니다.");
      cursors.add(page.nextCursor);
      page = await this.nativePage(page.nextCursor);
      if (page.revision !== revision || !Array.isArray(page.rows) || !page.rows.length) throw new Error("채팅 목록 원본이 갱신됐습니다.");
      chunks.push(page.rows);
    }
    const digest = createHash("sha256").update(JSON.stringify(meta));
    for (const rows of chunks) digest.update(JSON.stringify(rows));
    const fingerprint = digest.digest("hex");
    if (this.index?.digest !== fingerprint) this.index = assemble(meta, chunks, fingerprint);
    this.writes = this.writes.then(() => this.persist({ _id: "current", generation: revision, digest: fingerprint, meta, pages: chunks.length }, chunks))
      .catch(() => {});
  }

  private async persist(manifest: Manifest, chunks: Row[][]) {
    if (this.savedDigest === manifest.digest) return;
    const db = this.database.get();
    if (!db) return;
    const collection = db.collection<Chunk>("catalog_pages_v1");
    await collection.createIndex({ generation: 1, index: 1 });
    const options = { writeConcern: { w: 1, journal: true } };
    await collection.insertMany(chunks.map((rows, index) => ({
      _id: `${manifest.generation}.${index}`, generation: manifest.generation, index, rows,
    })), options);
    await db.collection<Manifest>("catalog_manifest_v1").replaceOne({ _id: "current" }, manifest, { ...options, upsert: true });
    this.savedDigest = manifest.digest;
    await collection.deleteMany({ generation: { $ne: manifest.generation } });
  }
}
