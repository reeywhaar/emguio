import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useConfirm } from "@app/components/Confirm";
import { mount } from "@app/test/harness";

/** A button that asks, and writes down what it was told. */
function Asker({ answers }: { answers: boolean[] }) {
  const confirm = useConfirm();
  return (
    <button
      type="button"
      onClick={async () =>
        answers.push(
          await confirm({
            title: "Delete this message for good?",
            message: "It cannot be brought back.",
            confirm: "Delete forever",
            danger: true,
          }),
        )
      }
    >
      Ask
    </button>
  );
}

const dialog = () => screen.getByRole("dialog");

describe("useConfirm", () => {
  it("names the button for what it does, and says yes through it", async () => {
    const answers: boolean[] = [];
    mount(<Asker answers={answers} />);
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));

    within(dialog()).getByText("It cannot be brought back.");
    fireEvent.click(
      within(dialog()).getByRole("button", { name: "Delete forever" }),
    );
    await waitFor(() => expect(answers).toEqual([true]));
  });

  it("says no when it is cancelled", async () => {
    const answers: boolean[] = [];
    mount(<Asker answers={answers} />);
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(answers).toEqual([false]));
  });

  // A ring on the button that destroys reads as armed, and Enter should not be the way.
  it("focuses neither button", () => {
    mount(<Asker answers={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));
    expect(document.activeElement?.tagName).not.toBe("BUTTON");
  });

  // A question still open when another is asked is answered no, not left hanging.
  it("answers an earlier question no when a second is asked", async () => {
    const answers: boolean[] = [];
    mount(<Asker answers={answers} />);
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));
    fireEvent.click(screen.getByRole("button", { name: "Ask", hidden: true }));
    await waitFor(() => expect(answers).toEqual([false]));
  });
});
