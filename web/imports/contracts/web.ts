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
    { name: "web.activity", mutation: false, model: false },
    { name: "web.thread", mutation: false, model: false },
    { name: "web.usage", mutation: false, model: false },
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
    { name: "web.guiQuitPreview", mutation: false, model: false },
    { name: "web.guiQuit", mutation: true, model: false },
    { name: "web.guiRelayStatus", mutation: false, model: false },
  ],
};

export type ChatItem = { id: string; type: string; text?: string; phase?: string; content?: Array<{ type: string; text?: string; [key: string]: unknown }>; [key: string]: unknown };
export type ChatTurn = { id: string; status: string; items: ChatItem[]; itemsView?: string; startedAt?: number; completedAt?: number; error?: { message: string } | null };
export type ChatThread = { id: string; name?: string; preview?: string; cwd: string; projectId?: string; status: { type: string }; updatedAt: number; recencyAt?: number; pinned?: boolean; archived?: boolean; externalOwner?: boolean; model?: string; reasoningEffort?: string; account?:string };
export type Project = { id: string; name: string; roots: Array<{ path: string }>; nativeId?:string };
export type Catalog = { threads: ChatThread[]; projects: Project[]; otherProjects?:Project[]; pinned: ChatThread[]; nextCursor?: string | null; accounts: Array<{ name: string; default?: boolean }>; defaultAccount?: string; guiQuitAvailable?: boolean; defaultCwd: string; total?:number };
export type PendingRequest = { request_id: string; method: string; params: Record<string, any> };
export type ChatHistory = {
  thread: ChatThread;
  turns: ChatTurn[];
  nextCursor?: string | null;
  queue: Array<{ id: string; input: Array<{ type: string; text?: string }> }>;
  pending: PendingRequest[];
  delivery?: { state: "ready" | "waiting" | "idle" | "unavailable" | "relay"; turnId?: string } | null;
  loginDraining?: boolean;
  loginGeneration?: number;
};
export type AccountLimits = { fiveHour: number | null; week: number | null; loading: boolean };

export function chatIdFromPath(path: string): string {
  const match = /^\/chat\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\/?$/i.exec(path);
  return match?.[1].toLowerCase() ?? "";
}

export function chatPath(id: string) {
  return id ? `/chat/${encodeURIComponent(id)}` : "/";
}

export type ChatActivity = {
  state: "running" | "responding" | "waiting" | "idle" | "unknown";
  source: "web" | "external";
  turnId?: string;
};
export type LiveTurn = {
  id: string;
  status: string;
  items: Array<{ id: string; phase?: string; text: string; streaming: boolean }>;
};
export type ChatActivitySnapshot = { writersKnown: boolean; threads: Record<string, ChatActivity>; turn?: LiveTurn; accountTurn?: boolean };

export function workingActivity(activity?: ChatActivity) {
  return activity?.state === "running" || activity?.state === "responding";
}

export function mergeLiveTurn(turns: ChatTurn[], live?: LiveTurn): ChatTurn[] {
  const current = live?.items?.filter(item => item.text);
  if (!live || !current?.length) return turns;
  const index = turns.findIndex(turn => turn.id === live.id);
  const saved = turns[index];
  const finished = saved?.status === "completed" || saved?.status === "failed";
  const items = new Map((saved?.items ?? []).map(item => [item.id, item]));
  for (const item of current) {
    if (finished && (item.phase !== "commentary" || items.has(item.id))) continue;
    if (!items.has(item.id) && saved?.items.some(other =>
      other.type === "agentMessage" && other.phase === item.phase && other.text === item.text
    )) continue;
    items.set(item.id, { ...item, type: "agentMessage", streaming: !finished && item.streaming });
  }
  if (finished && items.size === saved.items.length) return turns;
  const updated: ChatTurn = { ...saved, id: live.id, status: finished ? saved.status : live.status, items: [...items.values()] };
  return index < 0 ? [...turns, updated] : turns.map((turn, i) => i === index ? updated : turn);
}

export type ChatModel = {
  model: string;
  displayName: string;
  isDefault: boolean;
  defaultEffort: string;
  efforts: string[];
};

export type TurnSelection = { model: string; effort: string };
export const inheritTurnSelection: TurnSelection = { model: "", effort: "" };

export function resolveTurnSelection(chat: ChatThread, models: ChatModel[], selection: TurnSelection) {
  const chatModel = chat.model ? models.find(model => model.model === chat.model) : models.find(model => model.isDefault);
  const selectedModel = selection.model ? models.find(model => model.model === selection.model) : chatModel;
  const efforts = selectedModel?.efforts ?? [];
  const changedModel = !!selection.model && selection.model !== chat.model;
  const inheritedEffort = !changedModel && efforts.includes(chat.reasoningEffort || "")
    ? chat.reasoningEffort
    : selectedModel?.defaultEffort;
  return { chatModel, selectedModel, efforts, changedModel, inheritedEffort };
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
