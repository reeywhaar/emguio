/** What the API returns. Field names are the server's, unchanged. */

export type Me = {
  id: string;
  username: string;
  created_at: number;
};

/** How a server is secured: TLS from the first byte, or upgraded with STARTTLS. */
export type Security = "implicit" | "starttls";

export type Protocol = "imap";

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
  /** Who mail sent from it is from, beside the address; empty sends the address alone. */
  sender_name: string;
  incoming: Server & { protocol: Protocol };
  outgoing: Server | null;
  created_at: number;
  updated_at: number;
  /** When its mail was last brought up to date, null before the first time. */
  synced_at: number | null;
  /** Why the latest try did not, in a sentence; empty when it did. */
  sync_error: string;
};

/** A whole email config as the form sends it. An empty password is the one already saved. */
export type EmailConfigDraft = {
  name: string;
  email: string;
  sender_name: string;
  incoming: Server & { protocol: Protocol; password: string };
  outgoing: (Server & { password: string }) | null;
};

/** One server's answer to a test. message is empty when it signed in. */
export type Check = { ok: boolean; message: string };

export type TestResult = { incoming: Check; outgoing: Check | null };

/** What a mailbox is for, from the server's own flags or guessed from its name. */
export type SpecialUse =
  | "inbox"
  | "drafts"
  | "sent"
  | "archive"
  | "junk"
  | "trash"
  | "all"
  | "flagged"
  | "";

export type Mailbox = {
  id: string;
  /** The server's name, and the same split into the tree it describes. */
  name: string;
  path: string[];
  special_use: SpecialUse;
  /** False for one that only holds others. */
  selectable: boolean;
  messages: number;
  unseen: number;
};

export type Address = { name: string; email: string };

export type Message = {
  id: string;
  from: Address;
  to: Address[];
  subject: string;
  /** When it arrived, which is what the list is ordered by. */
  date: number;
  /** When the sender says it was sent, null when it says nothing. */
  sent: number | null;
  seen: boolean;
  flagged: boolean;
  answered: boolean;
  draft: boolean;
  has_attachments: boolean;
  preview: string;
};

export type MessagePage = { messages: Message[]; next_cursor?: string };

/** What a message's flags are on the server now. */
export type Flags = Pick<
  Message,
  "id" | "seen" | "flagged" | "answered" | "draft"
>;

/** A part of a message that is not its text. */
export type Part = {
  /** Where the server holds it, and what it is fetched by: "2", "1.3". */
  section: string;
  name: string;
  type: string;
  size: number;
  /** False for an image the HTML shows in place. */
  listed: boolean;
};

/** A message whole, for the reading pane. */
export type ReadMessage = Message & {
  cc: Address[];
  mailbox: string;
  /** Where its sender asks replies to go. */
  reply_to: Address[];
  text: string;
  /** The HTML as text, for a message with no text of its own: what a reply quotes. */
  html_text: string;
  /** Sanitized, and still a stranger's: shown only in a sandboxed frame. Empty for none. */
  html: string;
  /**
   * How many images from elsewhere are held back, which they are in Junk: each a blank image
   * until somebody asks, with the address it loads from in data-src. Elsewhere they load at
   * once, through the proxy, and this is 0.
   */
  held_images: number;
  parts: Part[];
};

/** Something asked to be done to a message, as the server is asked it. */
export type JobDraft = {
  email_config: string;
  mailbox: string;
  message: string;
  kind: "seen" | "flagged" | "move" | "delete";
  /** Set or cleared, for seen and flagged. */
  value: boolean;
  /** The mailbox a move goes to; empty otherwise. */
  target: string;
  /** Whether the message was read when asked, as drawn then: what its folders' counts move by. */
  seen: boolean;
};

/** A job the server has taken: waiting to be done, or failed with the sentence that says why. */
export type Job = JobDraft & {
  id: string;
  /** Empty while it waits. */
  error: string;
  created_at: number;
};

/** A message to send, as the server is asked it. */
export type Outgoing = {
  /** As typed: addresses apart by commas, each with a name or without. */
  to: string;
  cc: string;
  bcc: string;
  subject: string;
  text: string;
  /** Files from this device, base64. */
  attachments: { name: string; type: string; data: string }[];
  /** The message it answers: threaded under it, and marked answered. */
  reply: { mailbox: string; message: string } | null;
  /** The message it passes on, and the sections of its parts that go with it. */
  forward: { mailbox: string; message: string; parts: string[] } | null;
};
