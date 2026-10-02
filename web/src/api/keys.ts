/**
 * Every query key, in one object, hierarchically arranged so prefix invalidation is correct by
 * construction. Anything that belongs to one email config will sit under its id, so switching
 * configs cannot show one config's rows under another's name.
 */
export const qk = {
  me: ["me"] as const,
  /** The user's jobs, across every config: what is drawn as still to be done. */
  jobs: ["jobs"] as const,
  emailConfigs: ["email-configs"] as const,
  /** Everything read from every config's mail, for the event stream to invalidate at once. */
  mail: ["mail"] as const,
  mailboxes: (config: string) => ["mail", config, "mailboxes"] as const,
  /** Every folder's list of one config's messages. */
  lists: (config: string) => ["mail", config, "messages"] as const,
  /** A folder's list, or with q what the folder was searched for. */
  messages: (config: string, mailbox: string, q = "") =>
    ["mail", config, "messages", mailbox, q] as const,
  /** A folder's list and every search of it, for what changes them all. */
  folder: (config: string, mailbox: string) =>
    ["mail", config, "messages", mailbox] as const,
  /** Every message read whole from one config. */
  readings: (config: string) => ["mail", config, "message"] as const,
  message: (config: string, mailbox: string, message: string) =>
    ["mail", config, "message", mailbox, message] as const,
};

/** Mutation keys, so what is in flight can be asked about by prefix. */
export const mk = {
  /** Every action on a message, in any config. */
  actions: ["action"] as const,
  actionsOf: (config: string) => ["action", config] as const,
};
