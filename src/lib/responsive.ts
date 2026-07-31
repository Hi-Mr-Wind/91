import { useEffect, useState } from "react";

const MOBILE_LAYOUT_QUERY = "(max-width: 640px)";

export const MOBILE_VIDEO_PAGE_SIZE = 14;

export function useIsMobile(): boolean {
  const [mobile, setMobile] = useState(
    () => window.matchMedia(MOBILE_LAYOUT_QUERY).matches
  );

  useEffect(() => {
    const media = window.matchMedia(MOBILE_LAYOUT_QUERY);
    const handleChange = () => setMobile(media.matches);
    media.addEventListener("change", handleChange);
    return () => media.removeEventListener("change", handleChange);
  }, []);

  return mobile;
}
