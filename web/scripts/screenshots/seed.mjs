// What the screenshots show: one user, one mail account, and the mail in it.
//
// Invented, all of it, for people out of Dan Brown's novels. Adding a message or a folder is an
// entry here and nothing else: capture.mjs makes the folders and appends every message to the
// mail server, oldest first.
//
// A message is { key, folder, from, to?, subject, ago, body, ... }:
//   ago          how long before `now` it arrived: 25m, 3h, 2d, 1w
//   unread       not \Seen; flagged is \Flagged
//   replyTo      the key of the message it answers, which puts it in that conversation
//   attachments  [{ name, type, size }], filled with bytes of that size
//   html         an HTML part beside the text one
// `to` is Langdon when it is left out, and `from` is Langdon in Sent and Drafts.

// The moment the pictures are taken at. Fixed, so a run on another day draws the same dates.
export const now = "2026-06-17T10:30:00Z";

// Who signs in to emguio, through the first-run invitation.
export const account = {
  username: "robert",
  password: "a password nobody will ever type",
};

// The mail account added in Settings, on the servers capture.mjs runs as imap. and smtp.example.com.
export const mail = {
  email: "langdon@example.com",
  name: "Robert Langdon",
  password: "another password nobody types",
};

const langdon = `${mail.name} <${mail.email}>`;
const faukman = "Jonas Faukman <jonas@faukman.example>";
const sophie = "Sophie Neveu <sophie@neveu.example>";
const katherine = "Katherine Solomon <katherine@solomon.example>";
const fache = "Bezu Fache <fache@police.example>";

// Made in this order; the server's own are known by name.
export const folders = [
  "Drafts",
  "Sent",
  "Archive",
  "Junk",
  "Trash",
  "Harvard",
  "Publishing",
  "Travel",
];

// The message open in the mail shot.
export const opened = "manuscript3";

export const messages = [
  // A conversation over three days, from Inbox and Sent.
  {
    key: "manuscript1",
    folder: "INBOX",
    from: faukman,
    subject: "The new manuscript",
    ago: "2d",
    body: `Robert,

Marketing asks, gently, whether the book has an ending yet. I told them you were deciphering it.

Jonas`,
  },
  {
    key: "manuscript2",
    folder: "Sent",
    from: langdon,
    to: faukman,
    subject: "Re: The new manuscript",
    ago: "1d",
    replyTo: "manuscript1",
    body: `It has two endings, Jonas. I am choosing between them.

The chapter on the golden ratio is with you by Friday.

Robert`,
  },
  {
    key: "manuscript3",
    folder: "INBOX",
    from: faukman,
    subject: "Re: The new manuscript",
    ago: "25m",
    unread: true,
    replyTo: "manuscript2",
    attachments: [
      { name: "jacket-draft.pdf", type: "application/pdf", size: 184_000 },
    ],
    body: `One ending, please. Readers count.

Plan so far:

- Friday: the golden ratio chapter, as promised
- Monday: the cover meeting, where nobody may say "enigmatic"
- June 30: the deadline you swore on your Mickey Mouse watch

The jacket draft is attached. Design hid a Fibonacci spiral in it; I am told you will find it in under a minute.

Jonas`,
  },

  {
    key: "lunch",
    folder: "INBOX",
    from: sophie,
    subject: "Lunch by Saint-Sulpice?",
    ago: "20h",
    body: `I am in Paris on Thursday. The café by Saint-Sulpice, at one? I promise no cryptexes.`,
  },
  {
    key: "results",
    folder: "INBOX",
    from: katherine,
    subject: "The lab results, first look",
    ago: "3h",
    flagged: true,
    attachments: [
      { name: "noetic-results.csv", type: "text/csv", size: 9_600 },
    ],
    body: `The numbers held up a third time. Peter thinks I should publish; I think you should read it first.`,
  },
  {
    key: "watch",
    folder: "INBOX",
    from: "Brattle Street Watchmakers <repairs@brattle.example>",
    subject: "Your watch is ready",
    ago: "1h",
    unread: true,
    attachments: [
      { name: "receipt-4471.pdf", type: "application/pdf", size: 42_000 },
    ],
    body: `Your 1938 character watch is cleaned and running again. Mickey's left arm keeps perfect time.`,
  },
  {
    key: "lecture",
    folder: "INBOX",
    from: "Edmond Kirsch <edmond@kirsch.example>",
    subject: "You will want to be in Bilbao for this",
    ago: "1d",
    unread: true,
    body: `Saturday, at the Guggenheim. I will answer two questions at once. Your seat is in the front row.`,
    html: `<p>Saturday, at the Guggenheim. I will answer <b>two questions</b> at once.</p><p>Your seat is in the front row.</p>`,
  },
  {
    key: "boarding",
    folder: "INBOX",
    from: "Skylark Air <boarding@skylark.example>",
    subject: "Your boarding pass: Boston to Bilbao",
    ago: "2d",
    attachments: [
      { name: "boarding-pass.pdf", type: "application/pdf", size: 61_000 },
    ],
    body: `Flight SK 214, Friday 19 June, departs 18:45 from gate 12. Boarding closes 20 minutes before departure.`,
  },
  {
    key: "florence",
    folder: "INBOX",
    from: "Sienna Brooks <sienna@brooks.example>",
    subject: "Photos from Florence",
    ago: "3d",
    attachments: [
      { name: "duomo.jpg", type: "image/jpeg", size: 2_400_000 },
      { name: "ponte-vecchio.jpg", type: "image/jpeg", size: 2_100_000 },
    ],
    body: `Two good ones, out of about two hundred. You are frowning at a fresco in all the others.`,
  },
  {
    key: "louvre1",
    folder: "Sent",
    from: langdon,
    to: fache,
    subject: "About the Louvre",
    ago: "5d",
    body: `Captain Fache, I can be in Paris on the 24th, if there is anything left to clear up.`,
  },
  {
    key: "louvre2",
    folder: "INBOX",
    from: fache,
    subject: "Re: About the Louvre",
    ago: "4d",
    replyTo: "louvre1",
    body: `Monsieur Langdon, a few small questions only. The Grand Gallery, at nine. Come alone.`,
  },
  {
    key: "library",
    folder: "INBOX",
    from: "Widener Library <loans@widener.example>",
    subject: "Two books are due back on Monday",
    ago: "6d",
    body: `"The Divine Proportion" and "Symbols of the Sacred Feminine" are due on Monday 22 June.`,
  },
  {
    key: "pool",
    folder: "INBOX",
    from: "Harvard Athletics <pool@athletics.example>",
    subject: "Pool hours for the summer",
    ago: "1w",
    body: `The pool opens at 6:00 from Monday. Fifty laps before breakfast remain your own affair.`,
  },
  {
    key: "temple",
    folder: "INBOX",
    from: "Peter Solomon <peter@solomon.example>",
    subject: "The House of the Temple, on the 2nd",
    ago: "8d",
    body: `The tour is booked for the 2nd. Bring the little stone pyramid; Katherine wants another look.`,
  },
  {
    key: "teabing",
    folder: "INBOX",
    from: "Leigh Teabing <leigh@teabing.example>",
    subject: "Earl Grey, and a theory",
    ago: "9d",
    body: `Come to Château Villette for tea. I have a new theory, and a new butler.`,
  },

  {
    key: "faculty",
    folder: "Harvard",
    from: "Department of Symbology <office@symbology.example>",
    subject: "Faculty meeting moved to Thursday",
    ago: "4d",
    body: `Same room, same coffee.`,
  },
  {
    key: "royalties",
    folder: "Publishing",
    from: faukman,
    subject: "Royalty statement",
    ago: "3w",
    attachments: [
      { name: "statement-q1.pdf", type: "application/pdf", size: 38_000 },
    ],
    body: `Attached. The Italian edition is doing very well.`,
  },
  {
    key: "hotel",
    folder: "Travel",
    from: "Hotel Ritz Paris <reservations@ritz.example>",
    subject: "Your stay, 24 to 27 June",
    ago: "2w",
    body: `We look forward to welcoming you again. Check-in from 15:00.`,
  },
  {
    key: "vatican",
    folder: "Archive",
    from: "Vittoria Vetra <vittoria@vetra.example>",
    subject: "Antimatter, and an apology",
    ago: "5w",
    body: `The canister is back where it belongs. Rome owes you a dinner.`,
  },
  {
    key: "prize",
    folder: "Junk",
    from: "Prize Desk <winner@prizes.example>",
    subject: "You have been selected for a secret society!!!",
    ago: "2d",
    unread: true,
    body: `Claim your robe today.`,
  },
  {
    key: "draft",
    folder: "Drafts",
    from: langdon,
    to: sophie,
    subject: "Re: Lunch by Saint-Sulpice?",
    ago: "19h",
    replyTo: "lunch",
    body: `One o'clock is perfect. I will`,
  },
];
