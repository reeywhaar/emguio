/** What the API returns. Field names are the server's, unchanged. */

export type Me = {
  id: string;
  username: string;
  created_at: number;
};

/** How a server is secured: TLS from the first byte, or upgraded with STARTTLS. */
export type Security = "implicit" | "starttls";

export type Protocol = "imap" | "pop3";

export type Server = {
  host: string;
  port: number;
  tls: Security;
  /** Empty on an outgoing server that signs in as the incoming one does. */
  username: string;
};

export type EmailConfig = {
  id: string;
  /** Empty when the address is name enough. */
  name: string;
  email: string;
  incoming: Server & { protocol: Protocol };
  outgoing: Server | null;
  created_at: number;
  updated_at: number;
};

/** A whole email config as the form sends it. An empty password is the one already saved. */
export type EmailConfigDraft = {
  name: string;
  email: string;
  incoming: Server & { protocol: Protocol; password: string };
  outgoing: (Server & { password: string }) | null;
};

/** One server's answer to a test. message is empty when it signed in. */
export type Check = { ok: boolean; message: string };

export type TestResult = { incoming: Check; outgoing: Check | null };
