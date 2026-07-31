import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const layoutSource = readFileSync(new URL("../src/admin/AdminLayout.tsx", import.meta.url), "utf8");
const apiSource = readFileSync(new URL("../src/admin/api.ts", import.meta.url), "utf8");
const adminCss = readFileSync(new URL("../src/styles/admin.css", import.meta.url), "utf8");

test("available updates open a release notes dialog", () => {
  assert.match(apiSource, /releaseNotes\?: string/);
  assert.match(layoutSource, /const \[availableUpdate, setAvailableUpdate\] = useState<api\.UpdateCheck \| null>\(null\)/);
  assert.match(layoutSource, /if \(result\.hasUpdate\) \{\s*setAvailableUpdate\(result\)/);
  assert.match(layoutSource, /className="admin-modal--release-notes"/);
  assert.match(layoutSource, /aria-label="Release Note"/);
  assert.match(layoutSource, /availableUpdate\.releaseNotes\?\.trim\(\) \|\| "该版本未提供 Release Note。"/);
  assert.match(layoutSource, /href=\{availableUpdate\.releaseUrl\}/);
  assert.doesNotMatch(layoutSource, /onClick=\{\(\) => setAvailableUpdate\(null\)\}>\s*关闭\s*<\/button>/);
  assert.match(adminCss, /\.admin-release-notes__content div\s*\{[^}]*white-space:\s*pre-wrap/s);
  assert.match(adminCss, /\.admin-modal--release-notes\s*\{[^}]*border:\s*0;[^}]*box-shadow:\s*none;/s);
  assert.match(adminCss, /\.admin-modal--release-notes \.admin-modal__header,[\s\S]*?\.admin-modal--release-notes \.admin-modal__footer\s*\{[^}]*border:\s*0;/);
  assert.doesNotMatch(layoutSource, /dangerouslySetInnerHTML/);
});
