import { forwardRef } from "react";
import type { PreviewState } from "@/types";

type Props = {
  src: string;
  state: PreviewState;
  onCanPlay: () => void;
  onError: () => void;
  onTimeUpdate?: (progress: number) => void; // 0~1
};

// 底层 video 节点。只在父组件判定需要挂载时才渲染，卸载时父组件负责清理
export const PreviewVideo = forwardRef<HTMLVideoElement, Props>(
  function PreviewVideo(
    { src, state, onCanPlay, onError, onTimeUpdate },
    ref
  ) {
    return (
      <video
        ref={ref}
        className={`preview-video ${state === "playing" ? "is-visible" : ""}`}
        src={src}
        muted
        autoPlay
        loop
        playsInline
        preload="metadata"
        onCanPlay={onCanPlay}
        onError={onError}
        onTimeUpdate={(e) => {
          if (!onTimeUpdate) return;
          const el = e.currentTarget;
          if (el.duration > 0) {
            onTimeUpdate(el.currentTime / el.duration);
          }
        }}
        aria-hidden="true"
      />
    );
  }
);
