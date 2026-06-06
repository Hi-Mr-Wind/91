import assert from "node:assert/strict";
import test from "node:test";

import { defaultUploadTitleFromFileName } from "../src/lib/uploadTitle.ts";

test("uses the selected file name without the extension as the default upload title", () => {
  assert.equal(defaultUploadTitleFromFileName("holiday.clip.final.mp4"), "holiday.clip.final");
});

test("falls back to the full file name when there is no extension", () => {
  assert.equal(defaultUploadTitleFromFileName("clip"), "clip");
});
