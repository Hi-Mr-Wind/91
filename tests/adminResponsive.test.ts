import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const adminCss = readFileSync(
  new URL("../src/styles/admin.css", import.meta.url),
  "utf8"
);

function ruleBody(css: string, selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = css.match(new RegExp(`${escapedSelector}\\s*\\{([^}]*)\\}`));
  assert.ok(match, `Expected CSS rule for ${selector}`);
  return match[1];
}

function allRuleBodies(css: string, selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return Array.from(css.matchAll(new RegExp(`${escapedSelector}\\s*\\{([^}]*)\\}`, "g")))
    .map((match) => match[1])
    .join("\n");
}

// ruleBodyByContains 处理 CSS 里"多 selector 共享 body"的合并写法：
//   .a, .b, .c {
//     ...
//   }
// 上面的 `.b` 用直接的 `selector\s*\{` 正则匹不到。这里改成"找到任何包含目标
// selector 的连续 selector 列表（可含逗号 + 空白），紧跟一个 { ... } body"。
//
// 仅支持 body 内不再嵌套 `{}`（admin.css 没有 nesting，足够）。
function ruleBodyByContains(css: string, needle: string): string {
  const escapedNeedle = needle.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const re = new RegExp(`([^{}]*${escapedNeedle}[^{}]*)\\{([^}]*)\\}`, "g");
  const bodies: string[] = [];
  for (const m of css.matchAll(re)) {
    bodies.push(m[2]);
  }
  assert.ok(bodies.length > 0, `Expected at least one CSS rule containing ${needle}`);
  return bodies.join("\n");
}

function mobileCss(): string {
  const marker = "@media (max-width: 768px)";
  const start = adminCss.indexOf(marker);
  assert.notEqual(start, -1, "Expected mobile admin media query");
  return adminCss.slice(start);
}

test("admin login card fits narrow phone screens", () => {
  const body = ruleBody(adminCss, ".admin-login__card");

  // 桌面规则就用 min(...) 让窄屏自然适配；具体上限以 CSS 当前值为准（400px），
  // 关键是 `min(<某值>, 100%)` + `box-sizing: border-box`。
  assert.match(body, /width\s*:\s*min\(\d+px,\s*100%\)/);
  assert.match(body, /box-sizing\s*:\s*border-box/);
});

test("admin tables scroll inside the mobile viewport", () => {
  const css = mobileCss();
  // 视频/标签等"长内容"表的 mobile 形态：用 `.admin-table:not(.admin-drives-table)`
  // 把它们改成 display:block 卡片栈；网盘表 .admin-drives-table 走另一组 1280px 媒体
  // 查询。这里只断"非 drives 表的 mobile 卡片化"。
  const body = ruleBody(css, ".admin-table:not(.admin-drives-table)");

  assert.match(body, /display\s*:\s*block/);
});

test("admin modals and action footers adapt on mobile", () => {
  const css = mobileCss();

  // .admin-modal 桌面段已用 `width: min(620px, 100%)`，窄屏自然 100%；mobile 段
  // 只重写 max-height，所以这里断桌面规则即可。
  assert.match(ruleBody(adminCss, ".admin-modal"), /width\s*:\s*min\(\d+px,\s*100%\)/);
  // 多按钮 footer 在 mobile 下要换行避免溢出。
  assert.match(allRuleBodies(css, ".admin-modal__footer"), /flex-wrap\s*:\s*wrap/);
  // 表单 input/select/textarea 在 mobile 下铺满。规则用逗号合并写法（多 selector
  // 共享 body），所以走 ruleBodyByContains 而不是简单正则。
  assert.match(ruleBodyByContains(css, ".admin-form__row input"), /width\s*:\s*100%/);
});

test("mobile admin top navigation stays compact", () => {
  const css = mobileCss();

  assert.match(ruleBody(css, ".admin-shell"), /display\s*:\s*flex/);
  assert.match(ruleBody(css, ".admin-shell"), /flex-direction\s*:\s*column/);
  assert.match(ruleBody(css, ".admin-sidebar"), /height\s*:\s*48px/);
  assert.match(ruleBody(css, ".admin-sidebar"), /min-height\s*:\s*48px/);
  assert.match(ruleBody(css, ".admin-nav"), /align-items\s*:\s*center/);
  assert.match(ruleBody(css, ".admin-nav__link"), /height\s*:\s*34px/);
  assert.match(ruleBody(css, ".admin-nav__link"), /line-height\s*:\s*1/);
  assert.match(ruleBody(css, ".admin-nav__link"), /flex\s*:\s*0\s+0\s+auto/);
  assert.match(ruleBody(css, ".admin-main"), /padding\s*:\s*var\(--space-2\)\s+var\(--space-3\)\s+var\(--space-4\)/);
  assert.match(ruleBody(css, ".admin-page__header"), /margin-bottom\s*:\s*var\(--space-3\)/);
});
