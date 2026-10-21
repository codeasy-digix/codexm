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
    await assert.rejects(
      bridge.call("jobs.send", {}, "owner", "same-operation-id", true),
      (e: any) => e.code === "outcome_unknown",
    );
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
