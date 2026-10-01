/**
 * Every query key, in one object, hierarchically arranged so prefix invalidation is correct by
 * construction. Anything that belongs to one email config will sit under its id, so switching
 * configs cannot show one config's rows under another's name.
 */
export const qk = {
  me: ["me"] as const,
  emailConfigs: ["email-configs"] as const,
};
