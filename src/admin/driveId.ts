import type { AdminDrive } from "./api";

type DriveKind = AdminDrive["kind"];
type ExistingDrive = Pick<AdminDrive, "id"> | string;

function normalizeName(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function existingDriveId(drive: ExistingDrive): string {
  return typeof drive === "string" ? drive : drive.id;
}

export function makeUniqueDriveId(
  kind: DriveKind,
  name: string,
  existingDrives: ExistingDrive[]
): string {
  const used = new Set(existingDrives.map(existingDriveId));
  const base = normalizeName(name) || kind;
  let candidate = base;
  for (let suffix = 2; used.has(candidate); suffix += 1) {
    candidate = `${base}-${suffix}`;
  }
  return candidate;
}
