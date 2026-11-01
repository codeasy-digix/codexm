import { test } from "node:test";
import assert from "node:assert/strict";
import { preferredAccount, terminalFailure } from "../imports/contracts/selection.ts";

test("last used account defaults only when unknown or unregistered", () => {
  const accounts = [{name:"alpha"},{name:"beta"}];
  assert.equal(preferredAccount("beta", accounts), "beta");
  assert.equal(preferredAccount("default", accounts), "default");
  assert.equal(preferredAccount("removed", accounts), "default");
  assert.equal(preferredAccount(undefined, accounts), "default");
});
test("handoff error stays actionable above dead pane output", () => {
  const failure = terminalFailure({alive:false, exit_code:1, text:"Pane is dead (status 1)", launch:{error:"official archive failed; resume was not started"}});
  assert.match(failure!.title, /보관 단계 실패/);
  assert.match(failure!.help, /원래 화면/);
  assert.match(terminalFailure({alive:false, exit_code:7})!.title, /종료 코드 7/);
  assert.equal(terminalFailure({alive:true, exit_code:7}), null);
  assert.equal(terminalFailure({alive:false, exit_code:0}), null);
});
