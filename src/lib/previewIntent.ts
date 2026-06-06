export type PreviewIntentInput = {
  pointerType?: string;
  canHover: boolean;
  previewActive: boolean;
};

export function shouldInterceptPreviewTap(input: PreviewIntentInput): boolean {
  return isTouchLike(input.pointerType, input.canHover) && !input.previewActive;
}

export function shouldStartInstantPreview(input: {
  pointerType?: string;
}): boolean {
  return input.pointerType === "touch";
}

function isTouchLike(pointerType: string | undefined, canHover: boolean): boolean {
  if (pointerType === "mouse") return false;
  if (pointerType === "touch") return true;
  return !canHover;
}
