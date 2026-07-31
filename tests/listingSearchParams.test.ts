import assert from "node:assert/strict";
import test from "node:test";

import {
  readListingSort,
  withListingSort,
} from "../src/lib/listingSearchParams.ts";

test("listing sort is restored from the URL after returning from a video", () => {
  assert.equal(readListingSort(new URLSearchParams("sort=latest")), "latest");
  assert.equal(readListingSort(new URLSearchParams("sort=recent")), "recent");
  assert.equal(readListingSort(new URLSearchParams("sort=hot")), "hot");
  assert.equal(readListingSort(new URLSearchParams("sort=unknown")), "hot");
  assert.equal(readListingSort(new URLSearchParams()), "hot");
});

test("listing sort URL updates preserve filters and omit the default", () => {
  const original = new URLSearchParams("q=舞蹈&tag=推荐");

  const latest = withListingSort(original, "latest");
  assert.equal(latest.get("q"), "舞蹈");
  assert.equal(latest.get("tag"), "推荐");
  assert.equal(latest.get("sort"), "latest");
  assert.equal(original.get("sort"), null, "the current location must not be mutated");

  const hot = withListingSort(latest, "hot");
  assert.equal(hot.get("q"), "舞蹈");
  assert.equal(hot.get("tag"), "推荐");
  assert.equal(hot.get("sort"), null);
});
