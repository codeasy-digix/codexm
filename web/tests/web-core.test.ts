import test from "node:test";
import assert from "node:assert/strict";
import { chatSettingsShortcut, defaultChatShortcuts, composerAction,mergeTurns,chatTitle,type ChatTurn } from "../imports/contracts/web.ts";

test("composer distinguishes intervention, queue, IME and explicit newlines",()=>{
  assert.equal(composerAction({key:"Enter",shiftKey:false}),"immediate");
  assert.equal(composerAction({key:"Enter",shiftKey:false,metaKey:true}),"queue");
  assert.equal(composerAction({key:"Enter",shiftKey:true}),null);
  assert.equal(composerAction({key:"Enter",shiftKey:false,isComposing:true}),null);
  assert.equal(composerAction({key:"Enter",shiftKey:true,keyCode:229}),null);
  for(const key of ["altKey","ctrlKey"])assert.equal(composerAction({key:"Enter",shiftKey:false,[key]:true}),null);
  assert.equal(composerAction({key:"a",shiftKey:false}),null);
});

test("model and effort shortcuts use configured bindings and leave unassigned keys alone", () => {
  const command = (key: string) => ({ key, code: `Digit${key}`, metaKey: true, shiftKey: false, ctrlKey: false, altKey: false, isComposing: false, keyCode: 0, repeat: false });
  assert.deepEqual(chatSettingsShortcut(command("1"), defaultChatShortcuts), { kind: "model", value: "gpt-6.2-sol" });
  assert.deepEqual(chatSettingsShortcut(command("2"), defaultChatShortcuts), { kind: "model", value: "gpt-6.2-astra" });
  assert.equal(chatSettingsShortcut(command("3"), defaultChatShortcuts), undefined);
  for (const [digit, value] of [["4", "max"], ["5", "xhigh"], ["6", "high"]]) {
    assert.deepEqual(chatSettingsShortcut(command(digit), defaultChatShortcuts), { kind: "effort", value });
  }
  const binding = { kind: "effort", value: "ultra" } as const;
  const fallback = { ...command("7"), key: "&", metaKey: false, ctrlKey: true, shiftKey: true };
  assert.deepEqual(chatSettingsShortcut(fallback, { "7": binding }), binding);
  assert.equal(chatSettingsShortcut({ ...command("1"), isComposing: true }, defaultChatShortcuts), undefined);
  assert.equal(chatSettingsShortcut({ ...command("1"), metaKey: false }, defaultChatShortcuts), undefined);
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

test("native titles and empty drafts have useful display text",()=>{
  assert.equal(chatTitle(),"새 채팅");
  assert.equal(chatTitle({id:"one",cwd:"/fixture",updatedAt:0,status:{type:"idle"},preview:"first line\nrest"}),"first line");
});
