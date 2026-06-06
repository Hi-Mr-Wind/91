import { forwardRef } from "react";
import { ThumbsUp } from "lucide-react";
import type { CommentItem } from "@/types";
import { formatCount } from "@/lib/format";

type Props = {
  comments: CommentItem[];
};

export const CommentPanel = forwardRef<HTMLElement, Props>(function CommentPanel(
  { comments },
  ref
) {
  return (
    <section className="comment-panel" ref={ref} aria-label="评论">
      <header className="comment-panel__header">
        评论 ({comments.length})
      </header>
      <div className="comment-panel__body">
        {comments.length === 0 ? (
          <div className="comment-empty">暂无评论，快来抢沙发</div>
        ) : (
          <ul className="comment-list">
            {comments.map((c) => (
              <li key={c.id} className="comment-item">
                <div className="comment-item__meta">
                  <span className="comment-item__author">{c.author}</span>
                  <span>{c.createdAt}</span>
                  {typeof c.likes === "number" && (
                    <span>
                      <ThumbsUp
                        size={12}
                        style={{ verticalAlign: -1, marginRight: 2 }}
                      />
                      {formatCount(c.likes)}
                    </span>
                  )}
                </div>
                <div>{c.body}</div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
});
