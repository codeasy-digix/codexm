import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { BridgeClient } from "../server/bridge.ts";

test("bridge death marks an in-flight write unknown, reconnects and never resends it", async () => {
  const dir = mkdtempSync(join(tmpdir(), "codexm-bridge-"));
  const binary = join(dir, "bridge"),
    receipt = join(dir, "calls");
  writeFileSync(
    binary,
    `#!${process.execPath}
const fs = require('node:fs');
require('node:readline').createInterface({input:process.stdin}).on('line',line=>{
  const req=JSON.parse(line);
  fs.appendFileSync(${JSON.stringify(receipt)},req.method+'\\n');
  if(req.method==='jobs.send')process.exit(7);
  process.stdout.write(JSON.stringify({protocolVersion:1,id:req.id,result:{ready:true}})+'\\n');
});`,
    { mode: 0o700 },
  );
  const bridge = new BridgeClient(binary, join(dir, "config.toml"));
  try {
    assert.deepEqual(await bridge.call("capabilities"), { ready: true });
    const sent = bridge.call("jobs.send", {}, "owner", "first-operation-id", true);
    const queued = bridge.call("jobs.send", {}, "owner", "second-operation-id", true);
    await Promise.all([
      assert.rejects(sent, (e: any) => e.code === "outcome_unknown"),
      assert.rejects(queued, (e: any) => e.code === "service_unavailable"),
    ]);
    assert.deepEqual(await bridge.call("capabilities"), { ready: true });
    assert.equal(
      readFileSync(receipt, "utf8")
        .split("\n")
        .filter((v) => v === "jobs.send").length,
      1,
    );
  } finally {
    bridge.close();
    await new Promise((resolve) => setTimeout(resolve, 100));
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a pending history read can't delay a write behind the current request", async t => {
  const dir = mkdtempSync(join(tmpdir(), "codexm-bridge-"));
  const binary = join(dir, "bridge");
  writeFileSync(binary, `#!${process.execPath}
let order = 0;
require('node:readline').createInterface({ input: process.stdin }).on('line', line => {
  const { id } = JSON.parse(line);
  process.stdout.write(JSON.stringify({ protocolVersion: 1, id, result: ++order }) + '\\n');
});`, { mode: 0o700 });
  const bridge = new BridgeClient(binary, join(dir, "config.toml"));
  t.after(() => {
    bridge.close();
    rmSync(dir, { recursive: true, force: true });
  });
  const order = await Promise.all([
    bridge.call("web.historyPage"),
    bridge.call("web.catalogIndex"),
    bridge.call("web.send", {}, "owner", "first-operation-id", true),
    bridge.call("web.queueCancel", {}, "owner", "second-operation-id", true),
  ]);
  assert.deepEqual(order, [1, 4, 2, 3]);
});
