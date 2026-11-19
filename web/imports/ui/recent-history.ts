import type { ChatHistoryPage, ChatHistoryUpdate, HistoryBasis, RelayedInput } from "../contracts/web";

export type Transcript = ChatHistoryPage & { relayedInputs?: RelayedInput[] | null };

export function transcriptBasis(page?: Transcript): HistoryBasis {
  return { revision: page?.sync?.revision, turns: page?.sync?.order ?? [], relayDigest: page?.sync?.relayDigest };
}

export function updateTranscript(previous: Transcript | undefined, update: ChatHistoryUpdate): Transcript | undefined {
  const sync = update.sync;
  const relayedInputs = sync?.relayDigest && sync.relayDigest === previous?.sync?.relayDigest
    ? previous.relayedInputs : update.relayedInputs;
  if (sync?.unchanged) {
    if (!previous || previous.sync?.revision !== sync.revision) return;
    return { ...previous, sync: { ...sync, order: previous.sync?.order }, relayedInputs };
  }

  let latest = update.turns ?? [];
  if (sync?.order) {
    const received = new Map(latest.map(turn => [turn.id, turn]));
    const known = new Map(previous?.sync?.order?.map(turn => [turn.id, turn.digest]));
    const existing = new Map(previous?.turns.map(turn => [turn.id, turn]));
    latest = [];
    for (const { id, digest } of sync.order) {
      const turn = received.get(id) ?? (known.get(id) === digest ? existing.get(id) : undefined);
      if (!turn) return;
      latest.push(turn);
    }
  }

  // Previous pages survive a moving latest window while the windows overlap.
  // A long offline gap requires the new cursor so no turns are skipped.
  const prefix = latest.length ? previous?.turns.findIndex(turn => turn.id === latest[0].id) ?? -1 : -1;
  const turns = prefix > 0 && previous ? [...previous.turns.slice(0, prefix), ...latest] : latest;
  const prior = previous?.turns;
  return {
    turns: prior?.length === turns.length && turns.every((turn, i) => turn === prior[i]) ? prior : turns,
    nextCursor: prefix > 0 ? previous?.nextCursor : update.nextCursor,
    sync,
    relayedInputs,
  };
}

export class RecentHistory {
  private readonly pages = new Map<string, { page: Transcript; size: number }>();
  private readonly sizes = new WeakMap<object, number>();
  private bytes = 0;

  peek(thread: string) { return this.pages.get(thread)?.page; }

  private size(value: object) {
    const previous = this.sizes.get(value);
    if (previous !== undefined) return previous;
    const bytes = JSON.stringify(value).length * 2;
    this.sizes.set(value, bytes);
    return bytes;
  }

  retain(thread: string, page: Transcript) {
    const size = page.turns.reduce((total, turn) => total + this.size(turn), 0) + (page.relayedInputs ? this.size(page.relayedInputs) : 0);
    this.forget(thread);
    const budget = 16 * 1024 * 1024;
    if (size > budget) return;
    this.pages.set(thread, { page, size });
    this.bytes += size;
    for (const key of this.pages.keys()) {
      if (this.pages.size <= 12 && this.bytes <= budget) break;
      this.forget(key);
    }
  }

  forget(thread: string) {
    this.bytes -= this.pages.get(thread)?.size ?? 0;
    this.pages.delete(thread);
  }

  clear() { this.pages.clear(); this.bytes = 0; }
}
