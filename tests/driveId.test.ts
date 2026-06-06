import assert from "node:assert/strict";
import test from "node:test";

import { makeUniqueDriveId } from "../src/admin/driveId.ts";

test("generates readable drive ids from latin names", () => {
  assert.equal(makeUniqueDriveId("pikpak", "My PikPak", []), "my-pikpak");
});

test("falls back to drive kind when the name has no ascii id parts", () => {
  assert.equal(makeUniqueDriveId("p115", "主盘", []), "p115");
});

test("adds a suffix when the generated drive id already exists", () => {
  assert.equal(makeUniqueDriveId("p115", "115 主盘", ["115", "115-2"]), "115-3");
});
