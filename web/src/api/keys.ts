/**
 * Every query key, in one object, hierarchically arranged so prefix invalidation is correct by
 * construction. Anything that belongs to one email config will sit under its id, so switching
 * configs cannot show one config's rows under another's name.
 */
export const qk = {
  me: ["me"] as const,
  emailConfigs: ["email-configs"] as const,
  /** Everything read from every config's mail, for the event stream to invalidate at once. */
  mail: ["mail"] as const,
  mailboxes: (config: string) => ["mail", config, "mailboxes"] as const,
  messages: (config: string, mailbox: string) =>
    ["mail", config, "messages", mailbox] as const,
  message: (config: string, message: string, images: boolean) =>
    ["mail", config, "message", message, images] as const,
};
