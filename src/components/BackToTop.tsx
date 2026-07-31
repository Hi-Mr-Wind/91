import { useEffect, useState } from "react";
import { ArrowUp } from "lucide-react";

type Props = {
  onVisibilityChange?: (visible: boolean) => void;
};

export function BackToTop({ onVisibilityChange }: Props) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    function onScroll() {
      const nextVisible = window.scrollY > 400;
      setVisible((current) => {
        if (current !== nextVisible) {
          onVisibilityChange?.(nextVisible);
        }
        return nextVisible;
      });
    }
    window.addEventListener("scroll", onScroll, { passive: true });
    onScroll();
    return () => window.removeEventListener("scroll", onScroll);
  }, [onVisibilityChange]);

  return (
    <button
      className={`back-to-top ${visible ? "is-visible" : ""}`}
      onClick={() => window.scrollTo({ top: 0, behavior: "smooth" })}
      aria-label="返回顶部"
    >
      <ArrowUp size={18} />
    </button>
  );
}
