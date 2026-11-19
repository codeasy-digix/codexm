import test from "node:test";
import assert from "node:assert/strict";
import { composerAction,queueModifierForPlatform,mergeTurns,mergeLiveTurn,chatTitle,type ChatTurn } from "../imports/contracts/web.ts";
import { chatShortcutStorageKey, defaultChatShortcuts, readChatShortcuts, shortcutFromEvent, shortcutLabel } from "../imports/ui/chat-shortcuts.ts";

test("composer distinguishes platform queue shortcuts, intervention, IME and explicit newlines", () => {
  const enter = { key: "Enter", shiftKey: false };
  for (const [platform, modifier] of [["MacIntel", "Meta"], ["iPad", "Meta"], ["Win32", "Control"], ["Linux x86_64", "Control"]] as const) {
    assert.equal(queueModifierForPlatform(platform), modifier);
    const queueKey = modifier === "Meta" ? "metaKey" : "ctrlKey";
    const otherKey = modifier === "Meta" ? "ctrlKey" : "metaKey";
    assert.equal(composerAction(enter, modifier), "immediate");
    assert.equal(composerAction({ ...enter, [queueKey]: true }, modifier), "queue");
    assert.equal(composerAction({ ...enter, [otherKey]: true }, modifier), null);
    assert.equal(composerAction({ ...enter, metaKey: true, ctrlKey: true }, modifier), null);
    for (const keys of [{ shiftKey: true }, { altKey: true }, { isComposing: true }, { keyCode: 229 }, { key: "a" }]) {
      assert.equal(composerAction({ ...enter, ...keys }, modifier), null);
      assert.equal(composerAction({ ...enter, [queueKey]: true, ...keys }, modifier), null);
    }
  }
});

test("model and effort shortcut defaults use Command+Shift and reserve the composer keys", () => {
  const command = (code: string) => ({ code, metaKey: true, shiftKey: true, ctrlKey: false, altKey: false });
  const lookup = (key: string) => defaultChatShortcuts[shortcutFromEvent(command(`Digit${key}`))!];
  assert.deepEqual(lookup("0"), { kind: "reset" });
  assert.deepEqual(lookup("1"), { kind: "model", value: "gpt-6.2-sol" });
  assert.deepEqual(lookup("2"), { kind: "model", value: "gpt-6.2-astra" });
  assert.equal(lookup("3"), undefined);
  for (const [digit, value] of [["4", "max"], ["5", "xhigh"], ["6", "high"]]) {
    assert.deepEqual(lookup(digit), { kind: "effort", value });
  }
  assert.equal(defaultChatShortcuts[shortcutFromEvent({ ...command("Digit1"), shiftKey: false })!], undefined);
  const custom = shortcutFromEvent({ ...command("KeyK"), metaKey: false, shiftKey: false, ctrlKey: true, altKey: true });
  assert.equal(custom, "Control+Alt+KeyK");
  assert.equal(shortcutLabel(custom!), "Ctrl+Alt+K");
  for (const key of ["Enter", "NumpadEnter", "Tab", "Escape"]) assert.equal(shortcutFromEvent(command(key)), undefined);
  assert.equal(shortcutFromEvent({ ...command("KeyK"), isComposing: true }), undefined);
  assert.equal(shortcutFromEvent({ ...command("KeyK"), repeat: true }), undefined);
  assert.equal(shortcutFromEvent({ ...command("KeyK"), metaKey: false }), undefined);
  assert.equal(shortcutFromEvent({ ...command("KeyK"), getModifierState: name => name === "AltGraph" }), undefined);
});

test("legacy shortcuts keep their actions on Command+Shift and new preferences take precedence", () => {
  const action = { kind: "effort", value: "ultra" };
  const reset = { kind: "reset" };
  const entries: Record<string, string> = { "codexm.chat-shortcuts.v1": JSON.stringify({ 7: action }) };
  const storage = { getItem: (key: string) => entries[key] ?? null };
  assert.deepEqual(readChatShortcuts(storage), { "Meta+Shift+Digit7": action, "Meta+Shift+Digit0": reset });
  entries["codexm.chat-shortcuts.v2"] = JSON.stringify({ "Control+Shift+KeyJ": action });
  assert.deepEqual(readChatShortcuts(storage), { "Control+Shift+KeyJ": action, "Meta+Shift+Digit0": reset });
  entries["codexm.chat-shortcuts.v2"] = JSON.stringify({ "Meta+Shift+Digit0": action });
  assert.deepEqual(readChatShortcuts(storage), { "Meta+Shift+Digit0": action });
  entries[chatShortcutStorageKey] = JSON.stringify({ "Alt+Shift+KeyK": reset, "Meta+Enter": action, "Shift+KeyK": action });
  assert.deepEqual(readChatShortcuts(storage), { "Alt+Shift+KeyK": reset });
  entries[chatShortcutStorageKey] = "{}";
  assert.deepEqual(readChatShortcuts(storage), {});
});

test("older pages preserve current turns while live refresh replaces their content",()=>{
  const turn=(id:string,status="completed"):ChatTurn=>({id,status,items:[]});
  const current=[turn("two"),turn("three","inProgress")];
  const earlier=mergeTurns(current,[turn("one"),turn("two","interrupted")],true);
  assert.deepEqual(earlier.map(t=>t.id),["one","two","three"]);
  assert.equal(earlier[1].status,"completed");
  const next=mergeTurns(earlier,[turn("three"),turn("four")]);
  assert.deepEqual(next.map(t=>t.id),["one","two","three","four"]);
  assert.equal(next[2].status,"completed");
});

test("live reply replaces a preview by item ID, then yields to stored completion", () => {
  const user = { id: "user", type: "userMessage" };
  const reply = { id: "agent", type: "agentMessage", text: "First" };
  const active = { id: "turn", status: "inProgress", items: [user, reply] };
  const live = { id: "turn", status: "inProgress", items: [{ id: "agent", text: "First fragment", streaming: true }] };
  assert.deepEqual(mergeLiveTurn([active], live)[0].items.map(item => item.text), [undefined, "First fragment"]);
  const complete = { ...active, status: "completed", items: [user, { ...reply, text: "Complete" }] };
  assert.deepEqual(mergeLiveTurn([complete], live), [complete]);
  const note = { id: "progress", phase: "commentary", text: "Checking the queue", streaming: false };
  const savedNote = { ...note, type: "agentMessage" };
  const finishing = { ...live, items: [...live.items, note] };
  assert.deepEqual(mergeLiveTurn([complete], finishing)[0], { ...complete, items: [...complete.items, savedNote] });
  const stored = { ...complete, items: [...complete.items, savedNote] };
  assert.deepEqual(mergeLiveTurn([stored], finishing), [stored]);
});

test("native titles and empty drafts have useful display text",()=>{
  assert.equal(chatTitle(),"새 채팅");
  assert.equal(chatTitle({id:"one",cwd:"/fixture",updatedAt:0,status:{type:"idle"},preview:"first line\nrest"}),"first line");
});
