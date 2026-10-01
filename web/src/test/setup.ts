import { configure } from "@testing-library/react";

// The first render in a file is cold, and on a busy machine it passed testing-library's one
// second to wait for something that was on its way. A wait that will end is not a failure.
configure({ asyncUtilTimeout: 5000 });

/**
 * jsdom has no modal dialog, and Dialog calls showModal and close. Only what it needs: open
 * flipped, and close firing its event. Focus trapping and the top layer are a browser's to show.
 */
function showModal(this: HTMLDialogElement) {
  this.open = true;
}

function close(this: HTMLDialogElement) {
  this.open = false;
  this.dispatchEvent(new Event("close"));
}

if (typeof HTMLDialogElement !== "undefined") {
  HTMLDialogElement.prototype.showModal ??= showModal;
  HTMLDialogElement.prototype.close ??= close;
}
