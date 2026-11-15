import test from "node:test";
import assert from "node:assert/strict";
import { composerAction,mergeTurns,chatTitle,type ChatTurn } from "../imports/contracts/web.ts";

test("composer distinguishes intervention, queue, IME and explicit newlines",()=>{
  assert.equal(composerAction({key:"Enter",shiftKey:false}),"immediate");
  assert.equal(composerAction({key:"Enter",shiftKey:true}),"queue");
  assert.equal(composerAction({key:"Enter",shiftKey:false,isComposing:true}),null);
  assert.equal(composerAction({key:"Enter",shiftKey:true,keyCode:229}),null);
  for(const key of ["altKey","metaKey","ctrlKey"])assert.equal(composerAction({key:"Enter",shiftKey:false,[key]:true}),null);
  assert.equal(composerAction({key:"a",shiftKey:false}),null);
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
