import assert from "node:assert/strict";
import test from "node:test";
import { artifactRange } from "../server/artifacts";

test("private media responses support bounded single-byte ranges", () => {
  assert.deepEqual(artifactRange("bytes=3-6", 20), { start: 3, end: 6 });
  assert.deepEqual(artifactRange("bytes=3-", 20), { start: 3, end: 19 });
  assert.deepEqual(artifactRange("bytes=-7", 20), { start: 13, end: 19 });
  assert.deepEqual(artifactRange("bytes=16-999", 20), { start: 16, end: 19 });
  for (const value of ["bytes=20-", "bytes=4-3", "bytes=-0", "bytes=-", "bytes=0-1,3-4", "items=0-2"]) {
    assert.equal(artifactRange(value, 20), undefined, value);
  }
  assert.equal(artifactRange("bytes=0-", 0), undefined);
});
