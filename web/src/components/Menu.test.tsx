import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Menu, MenuItem } from "@app/components/Menu";

const menu = (onMove = vi.fn(), onElsewhere = vi.fn()) =>
  render(
    <div>
      <p onClick={onElsewhere}>elsewhere</p>
      <Menu name="Folder" label="More">
        {(close) => (
          <MenuItem
            onClick={() => {
              close();
              onMove();
            }}
          >
            Move…
          </MenuItem>
        )}
      </Menu>
    </div>,
  );

describe("Menu", () => {
  it("opens on its button and closes on a choice", () => {
    const onMove = vi.fn();
    menu(onMove);
    expect(screen.queryByRole("menu")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /^Folder:\s*More$/ }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Move…" }));
    expect(onMove).toHaveBeenCalled();
    expect(screen.queryByRole("menu")).toBeNull();
  });

  // What a press outside lands on is the page's cover, not what is under it.
  it("closes on Escape and on a press anywhere else, which reaches nothing", () => {
    const onElsewhere = vi.fn();
    menu(vi.fn(), onElsewhere);
    const button = screen.getByRole("button", { name: /More$/ });
    fireEvent.click(button);
    expect(button.getAttribute("aria-expanded")).toBe("true");
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();

    fireEvent.click(button);
    const veil = document.querySelector("[data-veil]")!;
    expect(veil.className).toContain("fixed inset-0");
    fireEvent.click(veil);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.querySelector("[data-veil]")).toBeNull();
    expect(onElsewhere).not.toHaveBeenCalled();
  });
});
