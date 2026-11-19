import type { ChatSkill } from "../contracts/web";

export type PromptDraft = { text: string; skills: ChatSkill[]; goal: boolean; pending?: string };
export const emptyPromptDraft: PromptDraft = { text: "", skills: [], goal: false };
const prefix = "codexm.promptDraft.";

export function readPromptDraft(thread: string): PromptDraft {
  try {
    const value = JSON.parse(localStorage.getItem(prefix + thread) || "null");
    if (!value || typeof value.text !== "string") return emptyPromptDraft;
    return {
      text: value.text,
      goal: value.goal === true,
      skills: Array.isArray(value.skills) ? value.skills.filter((skill: ChatSkill) =>
        skill && typeof skill.name === "string" && typeof skill.id === "string").slice(0, 16) : [],
      pending: typeof value.pending === "string" ? value.pending : undefined,
    };
  } catch { return emptyPromptDraft; }
}

export function writePromptDraft(thread: string, value: PromptDraft) {
  try {
    if (value.text || value.skills.length || value.goal) localStorage.setItem(prefix + thread, JSON.stringify(value));
    else localStorage.removeItem(prefix + thread);
  } catch { /* Leave the current editor usable if browser storage is disabled. */ }
}

export function settleStoredPrompt(thread: string, operation: string, accepted: boolean) {
  const saved = readPromptDraft(thread);
  if (saved.pending !== operation) return;
  writePromptDraft(thread, accepted ? emptyPromptDraft : { ...saved, pending: undefined });
}

export function slashRange(text: string, caret: number) {
  if (caret <= 0) return;
  const start = text.lastIndexOf("/", caret - 1);
  if (start < 0 || text[start - 1] === "/" || text[start - 1] === ":") return;
  const search = text.slice(start + 1, caret);
  if (!/^[\p{L}\p{N}_.:-]{0,48}$/u.test(search)) return;
  return { start, end: caret, search: search.toLowerCase() };
}
