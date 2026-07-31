import assert from "node:assert/strict";
import test from "node:test";
import {
  calculateFullscreenSubtitleBottom,
  getFullscreenPlayerOrientation,
} from "../src/lib/fullscreenSubtitleLayout";

function assertClose(actual: number | null, expected: number) {
  assert.notEqual(actual, null);
  assert.ok(Math.abs(actual! - expected) < 0.001, `${actual} != ${expected}`);
}

test("portrait fullscreen subtitles stay inside a contained landscape video", () => {
  assertClose(
    calculateFullscreenSubtitleBottom(390, 844, 1920, 1080),
    328.3125
  );
});

test("landscape fullscreen subtitles use a proportional media inset", () => {
  assertClose(
    calculateFullscreenSubtitleBottom(844, 390, 1920, 1080),
    23.4
  );
});

test("large fullscreen players cap the subtitle media inset", () => {
  assert.equal(
    calculateFullscreenSubtitleBottom(1920, 1080, 1920, 1080),
    56
  );
});

test("portrait videos include their contained bottom letterbox", () => {
  assertClose(
    calculateFullscreenSubtitleBottom(390, 844, 1080, 1920),
    116.93333333333334
  );
});

test("subtitle layout falls back when player or media dimensions are missing", () => {
  assert.equal(calculateFullscreenSubtitleBottom(0, 844, 1920, 1080), null);
  assert.equal(calculateFullscreenSubtitleBottom(390, 844, 0, 0), null);
  assert.equal(
    calculateFullscreenSubtitleBottom(390, Number.NaN, 1920, 1080),
    null
  );
});

test("player orientation follows the visual fullscreen dimensions", () => {
  assert.equal(getFullscreenPlayerOrientation(390, 844), "portrait");
  assert.equal(getFullscreenPlayerOrientation(844, 390), "landscape");
  assert.equal(getFullscreenPlayerOrientation(390, 390), "landscape");
  assert.equal(getFullscreenPlayerOrientation(0, 844), null);
});
