import { useState } from "react";
import { EyeOff, ThumbsDown, ThumbsUp } from "lucide-react";
import type { VideoDetail } from "@/types";
import { formatCount } from "@/lib/format";

type Props = {
  video: VideoDetail;
  onHideVideo: () => void;
  hideSaving?: boolean;
};

/**
 * 视频操作工具条。
 * - 整体是一张浮起的圆角玻璃卡，比上一版的横线分隔更"成体"。
 * - 点赞 + 点踩组成一个胶囊（中间一道竖线分隔），两侧分别带计数。
 * - "不再显示" 单独成一个次要按钮，hover 时露出 danger 色。
 *
 * 功能没变：
 * - 后端只有点赞接口（POST /api/video/:id/like），点踩仅本地 state。
 * - 失败回滚已经处理。
 */
export function VideoActions({ video, onHideVideo, hideSaving }: Props) {
  const [likes, setLikes] = useState(video.likes ?? 0);
  const [dislikes, setDislikes] = useState(video.dislikes ?? 0);
  const [bursting, setBursting] = useState(false);
  const [liked, setLiked] = useState(false);
  const [disliked, setDisliked] = useState(false);

  async function handleLike() {
    if (liked) return;
    setLiked(true);
    setLikes((n) => n + 1);
    setBursting(true);
    window.setTimeout(() => setBursting(false), 320);

    if (disliked) {
      setDisliked(false);
      setDislikes((n) => Math.max(0, n - 1));
    }

    try {
      const res = await fetch(
        `/api/video/${encodeURIComponent(video.id)}/like`,
        { method: "POST", credentials: "include" }
      );
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = (await res.json()) as { likes: number };
      if (typeof data.likes === "number") {
        setLikes(data.likes);
      }
    } catch {
      setLikes((n) => Math.max(0, n - 1));
      setLiked(false);
    }
  }

  function handleDislike() {
    if (disliked) {
      setDisliked(false);
      setDislikes((n) => Math.max(0, n - 1));
      return;
    }
    setDisliked(true);
    setDislikes((n) => n + 1);
    if (liked) {
      setLiked(false);
      setLikes((n) => Math.max(0, n - 1));
    }
  }

  return (
    <div className="vd-actions" role="toolbar" aria-label="视频操作">
      <div className="vd-actions__group" role="group" aria-label="点赞和点踩">
        <button
          type="button"
          className={`vd-actions__pill vd-actions__like${
            liked ? " is-active" : ""
          }${bursting ? " is-bursting" : ""}`}
          onClick={handleLike}
          aria-pressed={liked}
          aria-label="点赞"
        >
          <ThumbsUp size={18} fill={liked ? "currentColor" : "none"} />
          <span className="vd-actions__count">{formatCount(likes)}</span>
        </button>
        <span className="vd-actions__divider" aria-hidden="true" />
        <button
          type="button"
          className={`vd-actions__pill vd-actions__dislike${
            disliked ? " is-active" : ""
          }`}
          onClick={handleDislike}
          aria-pressed={disliked}
          aria-label="点踩"
        >
          <ThumbsDown size={18} fill={disliked ? "currentColor" : "none"} />
          <span className="vd-actions__count">{formatCount(dislikes)}</span>
        </button>
      </div>

      <button
        type="button"
        className="vd-actions__btn vd-actions__hide"
        onClick={onHideVideo}
        disabled={hideSaving}
        aria-label="不再显示这个视频"
      >
        <EyeOff size={16} />
        <span>{hideSaving ? "处理中" : "不再显示"}</span>
      </button>
    </div>
  );
}
