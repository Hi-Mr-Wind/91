export function defaultUploadTitleFromFileName(fileName: string): string {
  const baseName = fileName.split(/[\\/]/).pop()?.trim() ?? "";
  const lastDot = baseName.lastIndexOf(".");
  if (lastDot > 0) {
    return baseName.slice(0, lastDot);
  }
  return baseName;
}
