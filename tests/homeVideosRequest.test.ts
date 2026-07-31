import assert from "node:assert/strict";
import test from "node:test";
import {
  fetchHomeVideos,
  fetchListing,
  fetchTags,
  readCachedTags,
} from "../src/data/videos";

test("home recommendations send only the requested display count", async (t) => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  let requestPath = "";
  let requestInit: RequestInit | undefined;
  globalThis.fetch = (async (input, init) => {
    calls += 1;
    requestPath = String(input);
    requestInit = init;
    return new Response("[]", {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  t.after(() => {
    globalThis.fetch = originalFetch;
  });

  const result = await fetchHomeVideos(8);

  assert.deepEqual(result, []);
  assert.equal(calls, 1);
  assert.equal(
    requestPath,
    "/api/home?count=8"
  );
  assert.equal(requestInit?.method, undefined);
  assert.equal(requestInit?.body, undefined);
  assert.equal(requestInit?.cache, "no-store");
  assert.equal(new Headers(requestInit?.headers).get("Accept"), "application/json");
});

test("home recommendations retry one transient GET failure", async (t) => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = (async () => {
    calls += 1;
    if (calls === 1) {
      return new Response("unavailable", { status: 503 });
    }
    return new Response(JSON.stringify([{ id: "video-after-retry" }]), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  t.after(() => {
    globalThis.fetch = originalFetch;
  });

  const result = await fetchHomeVideos();

  assert.equal(calls, 2);
  assert.deepEqual(result.map((item) => item.id), ["video-after-retry"]);
});

test("home recommendations trust one server-filtered response", async (t) => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  let requestPath = "";
  globalThis.fetch = (async (input) => {
    calls += 1;
    requestPath = String(input);
    return new Response(JSON.stringify([
      { id: "first-new-video" },
      { id: "second-new-video" },
    ]), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  t.after(() => {
    globalThis.fetch = originalFetch;
  });

  const result = await fetchHomeVideos(12);

  assert.equal(calls, 1);
  assert.equal(requestPath, "/api/home?count=12");
  assert.deepEqual(
    result.map((item) => item.id),
    ["first-new-video", "second-new-video"]
  );
});

test("home recommendations keep the default request path short", async (t) => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  let requestPath = "";
  globalThis.fetch = (async (input) => {
    calls += 1;
    requestPath = String(input);
    return new Response("[]", {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  t.after(() => {
    globalThis.fetch = originalFetch;
  });

  const result = await fetchHomeVideos();

  assert.equal(calls, 1);
  assert.equal(requestPath, "/api/home");
  assert.deepEqual(result, []);
});

test("home recommendation request failures remain observable", async (t) => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = (async () => {
    calls += 1;
    return new Response("unavailable", { status: 503 });
  }) as typeof fetch;
  t.after(() => {
    globalThis.fetch = originalFetch;
  });

  await assert.rejects(() => fetchHomeVideos(), /HTTP 503/);
  assert.equal(calls, 2);
});

test("listing request failures are not converted to an empty library", async (t) => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = (async () => {
    calls += 1;
    return new Response("unauthorized", { status: 401 });
  }) as typeof fetch;
  t.after(() => {
    globalThis.fetch = originalFetch;
  });

  await assert.rejects(
    () => fetchListing(1, 96, { sort: "latest", includeTotal: false }),
    /HTTP 401/
  );
  assert.equal(calls, 1);
});

test("tags stay cached for the current browser session", async (t) => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  const responseTags = [{ id: "tag-1", label: "标签一", count: 3 }];
  globalThis.fetch = (async (input) => {
    calls += 1;
    assert.equal(String(input), "/api/tags");
    return new Response(JSON.stringify(responseTags), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  t.after(() => {
    globalThis.fetch = originalFetch;
  });

  const firstResult = await fetchTags();
  const secondResult = await fetchTags();

  assert.equal(calls, 1);
  assert.deepEqual(firstResult, responseTags);
  assert.strictEqual(secondResult, firstResult);
  assert.strictEqual(readCachedTags(), firstResult);
});
