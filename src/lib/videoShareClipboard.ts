import { createVideoShare } from "@/data/videos";

export type VideoShareCopyResult = {
  url: string;
  copied: boolean;
};

/**
 * 创建一次性分享链接，并在当前点击手势尚未结束时立即登记剪贴板写入。
 * WebKit 会在 await 网络请求后撤销用户手势；ClipboardItem 的延迟数据
 * 可以让 HTTPS 下的 iOS Safari 等链接生成后再完成同一次写入。
 */
export async function createAndCopyVideoShare(
  videoID: string
): Promise<VideoShareCopyResult> {
  const shareURLPromise = createVideoShare(videoID).then(
    ({ url }) => new URL(url, window.location.origin).href
  );
  // 必须位于第一个 await 之前，才能保留 iOS WebKit 的用户手势授权。
  const clipboardWrite = startShareClipboardWrite(shareURLPromise);
  const copiedPromise = clipboardWrite.then(
    () => true,
    () => false
  );
  const [url, copied] = await Promise.all([shareURLPromise, copiedPromise]);
  return { url, copied };
}

/** 已有链接的直接点击复制入口，供 HTTP/旧版 iOS 的第二次点击使用。 */
export function copyExistingVideoShareURL(value: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      return navigator.clipboard.writeText(value).catch((error) => {
        if (legacyCopyText(value)) return;
        throw error;
      });
    } catch (error) {
      if (legacyCopyText(value)) return Promise.resolve();
      return Promise.reject(error);
    }
  }
  return legacyCopyText(value)
    ? Promise.resolve()
    : Promise.reject(new Error("copy failed"));
}

function startShareClipboardWrite(valuePromise: Promise<string>): Promise<void> {
  if (navigator.clipboard?.write && typeof ClipboardItem !== "undefined") {
    try {
      const textBlob = valuePromise.then(
        (value) => new Blob([value], { type: "text/plain" })
      );
      return navigator.clipboard.write([
        new ClipboardItem({ "text/plain": textBlob }),
      ]);
    } catch {
      // 不支持延迟 ClipboardItem 的浏览器继续走普通复制。
    }
  }
  return valuePromise.then(copyExistingVideoShareURL);
}

function legacyCopyText(value: string): boolean {
  if (!document.body) return false;
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.readOnly = true;
  textarea.style.position = "fixed";
  textarea.style.top = "0";
  textarea.style.left = "0";
  textarea.style.width = "1px";
  textarea.style.height = "1px";
  textarea.style.padding = "0";
  textarea.style.border = "0";
  textarea.style.opacity = "0";
  textarea.style.fontSize = "16px";
  document.body.appendChild(textarea);
  try {
    textarea.focus({ preventScroll: true });
    textarea.select();
    textarea.setSelectionRange(0, value.length);
    return document.execCommand("copy");
  } finally {
    textarea.remove();
  }
}
