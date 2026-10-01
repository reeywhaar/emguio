import {
  createContext,
  useCallback,
  useContext,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";

export type ConfirmOptions = {
  title: string;
  /** What will happen, said plainly, which is the whole of what a confirmation is for. */
  message: ReactNode;
  /** The button that goes ahead, named for what it does: "Delete forever", not "OK". */
  confirm: string;
  /** Drawn as danger, for what cannot be taken back. */
  danger?: boolean;
};

type Ask = (options: ConfirmOptions) => Promise<boolean>;

const ConfirmContext = createContext<Ask | null>(null);

/**
 * Asking before something that cannot be taken back, in the application's own dialog rather
 * than the browser's: window.confirm has buttons labelled OK and Cancel and no room to say which
 * does what. This keeps its shape — ask, and await a yes or a no — and names the button for what
 * it does.
 *
 * Neither button takes focus: a ring on the button that destroys reads as armed, and Enter
 * should not be the way something is destroyed. Escape and the close are a no.
 *
 * One dialog for the island, mounted by the provider, rather than one per button that asks.
 */
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [asking, setAsking] = useState<ConfirmOptions | null>(null);
  const answer = useRef<(yes: boolean) => void>(() => {});

  const ask = useCallback<Ask>(
    (options) =>
      new Promise<boolean>((resolve) => {
        // A question still open when another is asked is answered no, rather than left
        // hanging with nothing ever resolving it.
        answer.current(false);
        answer.current = resolve;
        setAsking(options);
      }),
    [],
  );

  const settle = (yes: boolean) => {
    answer.current(yes);
    answer.current = () => {};
    setAsking(null);
  };

  return (
    <ConfirmContext.Provider value={ask}>
      {children}
      <Dialog
        open={asking !== null}
        onClose={() => settle(false)}
        title={asking?.title ?? ""}
        footer={
          <>
            <Button onClick={() => settle(false)}>Cancel</Button>
            <Button
              variant={asking?.danger ? "danger" : "solid"}
              onClick={() => settle(true)}
            >
              {asking?.confirm}
            </Button>
          </>
        }
      >
        <p className="text-sm">{asking?.message}</p>
      </Dialog>
    </ConfirmContext.Provider>
  );
}

/** Ask a question, and wait for the answer: true to go ahead. */
export function useConfirm(): Ask {
  const ask = useContext(ConfirmContext);
  if (!ask) throw new Error("useConfirm needs a ConfirmProvider above it.");
  return ask;
}
