import { bridgeContract as native } from "./native";

export const bridgeContract = {
  protocolVersion: native.protocolVersion,
  methods: [
    ...native.methods.filter(m=>!m.name.startsWith("terminal.")&&!m.name.startsWith("chats.")&&m.name!=="accounts.activate"),
    { name: "web.catalog", mutation: false, model: false },
    { name: "web.history", mutation: false, model: false },
    { name: "web.items", mutation: false, model: false },
    { name: "web.item", mutation: false, model: false },
    { name: "web.create", mutation: true, model: false },
    { name: "web.send", mutation: true, model: true },
    { name: "web.interrupt", mutation: true, model: false },
    { name: "web.pin", mutation: true, model: false },
    { name: "web.move", mutation: true, model: false },
    { name: "web.archive", mutation: true, model: false },
    { name: "web.delete", mutation: true, model: false },
    { name: "web.projectCreate", mutation: true, model: false },
    { name: "web.pending", mutation: false, model: false },
    { name: "web.respond", mutation: true, model: false },
  ],
};

export type ChatItem = { id: string; type: string; text?: string; phase?: string; content?: Array<{ type: string; text?: string; [key: string]: unknown }>; [key: string]: unknown };
export type ChatTurn = { id: string; status: string; items: ChatItem[]; itemsView?: string; startedAt?: number; completedAt?: number; error?: { message: string } | null };
export type ChatThread = { id: string; name?: string; preview?: string; cwd: string; projectId?: string; status: { type: string }; updatedAt: number; recencyAt?: number; pinned?: boolean; archived?: boolean; externalOwner?: boolean; model?: string; reasoningEffort?: string; account?:string };
export type Project = { id: string; name: string; roots: Array<{ path: string }> };
export type Catalog = { threads: ChatThread[]; projects: Project[]; pinned: ChatThread[]; nextCursor?: string | null; accounts: Array<{ name: string; default?: boolean }>; defaultCwd: string };
export type PendingRequest = { request_id: string; method: string; params: Record<string, any> };
export type ChatHistory = { thread: ChatThread; turns: ChatTurn[]; nextCursor?: string | null; queue: Array<{ id: string; input: Array<{ type: string; text?: string }> }>; pending: PendingRequest[] };

export function composerAction(e: { key: string; shiftKey: boolean; isComposing?: boolean; keyCode?: number; ctrlKey?: boolean; altKey?: boolean; metaKey?: boolean }): "immediate" | "queue" | null {
  if (e.key !== "Enter" || e.isComposing || e.keyCode === 229 || e.ctrlKey || e.altKey || e.metaKey) return null;
  return e.shiftKey ? "queue" : "immediate";
}

export function mergeTurns(existing: ChatTurn[],incoming: ChatTurn[],older = false): ChatTurn[] {
  const result = new Map<string,ChatTurn>();
  for (const turn of older ? incoming : existing) result.set(turn.id,turn);
  for (const turn of older ? existing : incoming) result.set(turn.id,turn);
  return [...result.values()];
}

export function chatTitle(chat?: ChatThread | null) { return chat?.name || chat?.preview?.split("\n")[0]?.slice(0,90) || "새 채팅"; }
