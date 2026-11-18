import { bridgeContract as native } from "./native";

export const bridgeContract = {
  protocolVersion: native.protocolVersion,
  methods: [
    ...native.methods.filter(m=>!m.name.startsWith("terminal.")&&!m.name.startsWith("chats.")&&m.name!=="accounts.activate"),
    { name: "web.catalog", mutation: false, model: false },
    { name: "web.history", mutation: false, model: false },
    { name: "web.items", mutation: false, model: false },
    { name: "web.item", mutation: false, model: false },
    { name: "web.models", mutation: false, model: false },
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
export type Project = { id: string; name: string; roots: Array<{ path: string }>; nativeId?:string };
export type Catalog = { threads: ChatThread[]; projects: Project[]; otherProjects?:Project[]; pinned: ChatThread[]; nextCursor?: string | null; accounts: Array<{ name: string; default?: boolean }>; defaultCwd: string; total?:number };
export type PendingRequest = { request_id: string; method: string; params: Record<string, any> };
export type ChatHistory = { thread: ChatThread; turns: ChatTurn[]; nextCursor?: string | null; queue: Array<{ id: string; input: Array<{ type: string; text?: string }> }>; pending: PendingRequest[] };

export type ChatModel = {
  model: string;
  displayName: string;
  isDefault: boolean;
  defaultEffort: string;
  efforts: string[];
};

export type TurnSelection = { model: string; effort: string };
export const inheritTurnSelection: TurnSelection = { model: "", effort: "" };
export const shortcutDigits = ["1", "2", "3", "4", "5", "6", "7", "8", "9"] as const;
type ShortcutDigit = typeof shortcutDigits[number];
export type ShortcutAction = { kind: "model" | "effort"; value: string };
export type ChatShortcuts = Partial<Record<ShortcutDigit, ShortcutAction>>;
export const defaultChatShortcuts: ChatShortcuts = {
  "1": { kind: "model", value: "gpt-6.2-sol" },
  "2": { kind: "model", value: "gpt-6.2-astra" },
  "4": { kind: "effort", value: "max" },
  "5": { kind: "effort", value: "xhigh" },
  "6": { kind: "effort", value: "high" },
};

export function chatSettingsShortcut(event: Pick<KeyboardEvent,
  "key" | "code" | "metaKey" | "ctrlKey" | "shiftKey" | "altKey" | "repeat" | "isComposing" | "keyCode"
>, shortcuts: ChatShortcuts): ShortcutAction | undefined {
  if (event.isComposing || event.keyCode === 229 || event.altKey || event.repeat) return;
  const command = event.metaKey && !event.ctrlKey && !event.shiftKey;
  // Some browsers consume Command+number for tab navigation before dispatch.
  const fallback = event.ctrlKey && event.shiftKey && !event.metaKey;
  if (!command && !fallback) return;
  const key = /^(?:Digit|Numpad)([1-9])$/.exec(event.code)?.[1] ?? event.key;
  if (!/^[1-9]$/.test(key)) return;
  return shortcuts[key as ShortcutDigit];
}

export function effortLabel(effort: string): string {
  return effort === "xhigh" ? "Extra High" : effort.charAt(0).toUpperCase() + effort.slice(1);
}

export function composerAction(e: { key: string; shiftKey: boolean; isComposing?: boolean; keyCode?: number; ctrlKey?: boolean; altKey?: boolean; metaKey?: boolean }): "immediate" | "queue" | null {
  if (e.key !== "Enter" || e.isComposing || e.keyCode === 229 || e.ctrlKey || e.altKey || e.shiftKey) return null;
  return e.metaKey ? "queue" : "immediate";
}

export function mergeTurns(existing: ChatTurn[],incoming: ChatTurn[],older = false): ChatTurn[] {
  const result = new Map<string,ChatTurn>();
  for (const turn of older ? incoming : existing) result.set(turn.id,turn);
  for (const turn of older ? existing : incoming) result.set(turn.id,turn);
  return [...result.values()];
}

export function chatTitle(chat?: ChatThread | null) { return chat?.name || chat?.preview?.split("\n")[0]?.slice(0,90) || "새 채팅"; }
