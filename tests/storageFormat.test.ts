import assert from "node:assert/strict";
import test from "node:test";

import { formatBytes } from "../src/admin/storageFormat.ts";

test("formats byte counts for storage usage display", () => {
  assert.equal(formatBytes(0), "0 B");
  assert.equal(formatBytes(512), "512 B");
  assert.equal(formatBytes(1536), "1.5 KB");
  assert.equal(formatBytes(2 * 1024 * 1024), "2 MB");
  assert.equal(formatBytes(3.25 * 1024 * 1024 * 1024), "3.3 GB");
});
