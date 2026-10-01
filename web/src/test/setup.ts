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
