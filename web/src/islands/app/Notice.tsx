import { useEffect } from "react";

import { Cross } from "@app/components/icons";
import { dismiss, useNotice } from "@app/islands/app/notices";

/** How long a notice stays, unless closed: long enough to read twice. */
const STAYS = 8000;

/** The island's notice, at the foot of the screen. */
export function Notice() {
  const notice = useNotice();
  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => dismiss(notice.id), STAYS);
    return () => clearTimeout(timer);
  }, [notice]);
  if (!notice) return null;
  return (
    <div
      role="alert"
      className="fixed inset-x-4 bottom-4 z-10 mx-auto flex max-w-md items-start gap-3 rounded-lg border border-line bg-bg px-4 py-3 text-sm shadow-lg"
    >
      <p className="flex-1">{notice.text}</p>
      <button
        type="button"
        aria-label="Close"
        onClick={() => dismiss(notice.id)}
        className="-mr-1 shrink-0 rounded-md p-0.5 text-faint hover:bg-fill hover:text-fg"
      >
        <Cross />
      </button>
    </div>
  );
}
