import type { SortKey } from "../types";

export function readListingSort(params: URLSearchParams): SortKey {
  const sort = params.get("sort");
  switch (sort) {
    case "latest":
    case "recent":
    case "hot":
      return sort;
    default:
      return "hot";
  }
}

export function withListingSort(
  params: URLSearchParams,
  sort: SortKey
): URLSearchParams {
  const next = new URLSearchParams(params);
  if (sort === "hot") {
    next.delete("sort");
  } else {
    next.set("sort", sort);
  }
  return next;
}
