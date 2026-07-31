import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const installSource = readFileSync(
  new URL("../install.sh", import.meta.url),
  "utf8"
);

test("installer bypasses proxy settings for local service health checks", () => {
  assert.match(
    installSource,
    /local_service_curl\(\) \{[\s\S]*?curl --disable --noproxy '\*' "\$@"/
  );
  assert.match(
    installSource,
    /if local_service_curl -fsS --connect-timeout 2 --max-time 5 "\$url"/
  );
  assert.doesNotMatch(
    installSource,
    /if curl -fsS --connect-timeout 2 --max-time 5 "\$url"/
  );
});

test("installer distinguishes readiness failures from process start failures", () => {
  assert.match(
    installSource,
    /service process is active, but its local health endpoint is unreachable/
  );
  assert.match(
    installSource,
    /listener\(s\) found on port \$port/
  );
  assert.match(
    installSource,
    /service readiness check failed; see diagnostics above/
  );
  assert.doesNotMatch(installSource, /die "service failed to start"/);
});
