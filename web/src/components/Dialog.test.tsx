import { useState } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Dialog } from "@app/components/Dialog";
import { mount } from "@app/test/harness";

function Breaks(): never {
  throw new Error("the data was not what it claimed");
}

afterEach(() => vi.restoreAllMocks());

function Shuttable() {
  const [open, setOpen] = useState(true);
  return (
    <>
      <button type="button" onClick={() => setOpen(false)}>
        Shut
      </button>
      <Dialog open={open} onClose={() => setOpen(false)} title="Folders">
        <p>Inside</p>
      </Dialog>
    </>
  );
}

describe("a dialog", () => {
  // What it holds stays while it fades out, then goes, so a form starts empty the next time.
  it("keeps what it holds while it fades out, then lets it go", async () => {
    mount(<Shuttable />);
    fireEvent.click(screen.getByRole("button", { name: "Shut" }));
    screen.getByText("Inside");
    await waitFor(() => expect(screen.queryByText("Inside")).toBeNull());
  });

  // The close event fires for a close the caller made too, which is no one leaving.
  it("tells only of a close its caller did not make", () => {
    const onClose = vi.fn();
    function Caller() {
      const [open, setOpen] = useState(true);
      return (
        <>
          <button type="button" onClick={() => setOpen((o) => !o)}>
            Toggle
          </button>
          <Dialog open={open} onClose={onClose} title="Folders">
            <p>Inside</p>
          </Dialog>
        </>
      );
    }
    mount(<Caller />);
    fireEvent.click(screen.getByRole("button", { name: "Toggle" }));
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Toggle" }));
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  // showModal focuses the first control it finds, and a ring on Delete reads as armed.
  it("leaves nothing lit when no field asked for focus", () => {
    mount(
      <Dialog
        open
        onClose={vi.fn()}
        title="Mail account"
        footer={<button type="button">Delete</button>}
      >
        <input aria-label="Host" />
      </Dialog>,
    );
    expect(document.activeElement?.tagName).toBe("DIALOG");
  });

  it("gives focus to a field that asked for it", () => {
    mount(
      <Dialog
        open
        onClose={vi.fn()}
        title="Add mail account"
        footer={<button type="button">Cancel</button>}
      >
        <input data-autofocus aria-label="Email address" />
      </Dialog>,
    );
    expect(document.activeElement).toBe(screen.getByLabelText("Email address"));
  });

  it("keeps the title out of the part that scrolls", () => {
    const { container } = mount(
      <Dialog open onClose={vi.fn()} title="Mail account">
        <p>Something long</p>
      </Dialog>,
    );
    const scroller = container.querySelector(".overflow-y-auto")!;
    const heading = screen.getByRole("heading", { name: "Mail account" });
    expect(scroller.contains(heading)).toBe(false);
    expect(scroller.textContent).toBe("Something long");
  });

  it("puts the aside beside the title, out of the part that scrolls", () => {
    const { container } = mount(
      <Dialog
        open
        onClose={vi.fn()}
        title="Mail account"
        aside={<span>ec_1</span>}
      >
        <p>Something</p>
      </Dialog>,
    );
    const id = screen.getByText("ec_1");
    const heading = screen.getByRole("heading", { name: "Mail account" });
    expect(heading.parentElement!.contains(id)).toBe(true);
    expect(container.querySelector(".overflow-y-auto")!.contains(id)).toBe(
      false,
    );
  });

  it("keeps its title and close when the body fails", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const onClose = vi.fn();
    mount(
      <Dialog open onClose={onClose} title="Mail account">
        <Breaks />
      </Dialog>,
    );
    screen.getByText(/could not be shown/);
    screen.getByRole("button", { name: "Close" }).click();
    expect(onClose).toHaveBeenCalled();
  });

  // Counted: the inner of two dialogs closing leaves the page held for the outer.
  it("holds the page still while open, and hands it back once the last one shuts", () => {
    document.body.style.overflow = "auto";
    const outer = mount(
      <Dialog open onClose={vi.fn()} title="Outer">
        <p>Outer</p>
      </Dialog>,
    );
    const inner = mount(
      <Dialog open onClose={vi.fn()} title="Inner">
        <p>Inner</p>
      </Dialog>,
    );
    expect(document.body.style.overflow).toBe("hidden");
    inner.unmount();
    expect(document.body.style.overflow).toBe("hidden");
    outer.unmount();
    expect(document.body.style.overflow).toBe("auto");
    document.body.style.overflow = "";
  });
});
