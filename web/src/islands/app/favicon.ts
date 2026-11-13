/** The tab's icon: the envelope, with a dot while any mail account has unread mail. */
export function markUnread(unread: boolean) {
  const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  const href = unread ? "/favicon-unread.svg" : "/favicon.svg";
  if (link && link.getAttribute("href") !== href) {
    link.setAttribute("href", href);
  }
}
