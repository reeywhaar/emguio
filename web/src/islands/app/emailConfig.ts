import type { EmailConfig, EmailConfigDraft } from "@app/api/types";

/** What an email config is called wherever there is room for one line. */
export function labelOf(config: EmailConfig): string {
  return config.name || config.email;
}

/** What an account reads and sends with, a word each. */
export function serversOf(config: EmailConfig): string {
  const incoming = config.incoming.protocol.toUpperCase();
  return config.outgoing ? `${incoming} · SMTP` : incoming;
}

/** A saved config as a draft, with its passwords left as the ones saved. */
export function draftOf(config: EmailConfig): EmailConfigDraft {
  return {
    name: config.name,
    email: config.email,
    sender_name: config.sender_name,
    incoming: { ...config.incoming, password: "" },
    outgoing: config.outgoing ? { ...config.outgoing, password: "" } : null,
  };
}

const remembered = "emguio.email-config";

/** The config last looked at in this browser, so the app opens where it was left. */
export function lastConfig(): string | null {
  try {
    return window.localStorage.getItem(remembered);
  } catch {
    return null;
  }
}

export function rememberConfig(id: string) {
  try {
    window.localStorage.setItem(remembered, id);
  } catch {
    // A private window: the app opens on the first config instead.
  }
}

/** Which config a view is about: the one named, else the last looked at, else the first. */
export function pick(
  configs: EmailConfig[],
  named: string | null,
): EmailConfig | undefined {
  if (named) return configs.find((c) => c.id === named);
  const last = lastConfig();
  return configs.find((c) => c.id === last) ?? configs[0];
}
