import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Dialog } from "@app/components/Dialog";
import { mount } from "@app/test/harness";

function Breaks(): never {
  throw new Error("the data was not what it claimed");
}

afterEach(() => vi.restoreAllMocks());

describe("a dialog", () => {
  // showModal focuses the first control it finds, and a ring on Delete reads as armed.
  it("leaves nothing lit when no field asked for focus", () => {
    mount(
      <Dialog
        open
        onClose={vi.fn()}
        title="Email config"
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
        title="Add email config"
        footer={<button type="button">Cancel</button>}
      >
        <input data-autofocus aria-label="Email address" />
      </Dialog>,
    );
    expect(document.activeElement).toBe(screen.getByLabelText("Email address"));
  });

  it("keeps the title out of the part that scrolls", () => {
    const { container } = mount(
      <Dialog open onClose={vi.fn()} title="Email config">
        <p>Something long</p>
      </Dialog>,
    );
    const scroller = container.querySelector(".overflow-y-auto")!;
    const heading = screen.getByRole("heading", { name: "Email config" });
    expect(scroller.contains(heading)).toBe(false);
    expect(scroller.textContent).toBe("Something long");
  });

  it("puts the aside beside the title, out of the part that scrolls", () => {
    const { container } = mount(
      <Dialog
        open
        onClose={vi.fn()}
        title="Email config"
        aside={<span>ec_1</span>}
      >
        <p>Something</p>
      </Dialog>,
    );
    const id = screen.getByText("ec_1");
    const heading = screen.getByRole("heading", { name: "Email config" });
    expect(heading.parentElement!.contains(id)).toBe(true);
    expect(container.querySelector(".overflow-y-auto")!.contains(id)).toBe(
      false,
    );
  });

  it("keeps its title and close when the body fails", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const onClose = vi.fn();
    mount(
      <Dialog open onClose={onClose} title="Email config">
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
