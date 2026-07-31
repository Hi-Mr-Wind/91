const SUBTITLE_MEDIA_INSET_RATIO = 0.06;
const SUBTITLE_MEDIA_INSET_MIN_PX = 16;
const SUBTITLE_MEDIA_INSET_MAX_PX = 56;

export type FullscreenPlayerOrientation = "portrait" | "landscape";

export function getFullscreenPlayerOrientation(
  playerWidth: number,
  playerHeight: number
): FullscreenPlayerOrientation | null {
  if (!isPositiveFinite(playerWidth) || !isPositiveFinite(playerHeight)) {
    return null;
  }
  return playerHeight > playerWidth ? "portrait" : "landscape";
}

/**
 * Returns the distance from the player bottom to a subtitle placed just inside
 * the visible video frame when the video uses object-fit: contain.
 */
export function calculateFullscreenSubtitleBottom(
  playerWidth: number,
  playerHeight: number,
  videoWidth: number,
  videoHeight: number
) {
  if (
    !isPositiveFinite(playerWidth) ||
    !isPositiveFinite(playerHeight) ||
    !isPositiveFinite(videoWidth) ||
    !isPositiveFinite(videoHeight)
  ) {
    return null;
  }

  const scale = Math.min(playerWidth / videoWidth, playerHeight / videoHeight);
  const renderedVideoHeight = videoHeight * scale;
  const bottomLetterbox = Math.max(0, (playerHeight - renderedVideoHeight) / 2);
  const mediaInset = clamp(
    renderedVideoHeight * SUBTITLE_MEDIA_INSET_RATIO,
    SUBTITLE_MEDIA_INSET_MIN_PX,
    SUBTITLE_MEDIA_INSET_MAX_PX
  );

  return bottomLetterbox + mediaInset;
}

function isPositiveFinite(value: number) {
  return Number.isFinite(value) && value > 0;
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}
