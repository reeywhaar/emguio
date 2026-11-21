// The demo's mail: one person, Robert Langdon, with five accounts on one invented mail server.
// Invented, all of it. An account is { name, email, folders, messages }; a message is as
// web/scripts/mail.mjs describes it.
import {
  folders as personalFolders,
  messages as personalMessages,
} from "../web/scripts/screenshots/seed.mjs";

// Who signs in to emguio, and the seal key that opens their mail passwords after each start.
export const user = { username: "demo", password: "demodemo" };
export const sealKey = "demodemodemo";

// What every mail account here signs in with: the mail server takes any address with it.
export const password = "demodemo";

export const owner = "Robert Langdon";

const own = ["Drafts", "Sent", "Archive", "Junk", "Trash"];

const harvard = "langdon@harvard.example";
const books = "robert@langdon-books.example";
const dean = "Office of the Dean <dean@harvard.example>";
const mara = "Mara Ellison <mara.ellison@harvard.example>";
const theo = "Theo Brandt <theo.brandt@harvard.example>";
const library = "Widener Library <holds@library.harvard.example>";
const it = "Harvard IT <noreply@it.harvard.example>";
const faukman = "Jonas Faukman <jonas@faukman.example>";
const marketing = "Lena Ortiz <lena@faukman.example>";
const rights = "Giulia Ferri <giulia@rights.example>";
const tour = "Event Desk <events@faukman.example>";

const harvardMail = [
  {
    key: "meeting",
    folder: "INBOX",
    from: dean,
    subject: "Faculty meeting moved to Thursday",
    ago: "2h",
    unread: true,
    body: `Dear colleagues,

The faculty meeting moves from Tuesday to Thursday at 16:00, same room. The agenda is unchanged.

The Office of the Dean`,
  },
  {
    key: "thesis",
    folder: "Students",
    from: mara,
    subject: "Office hours: thesis on Mithraic symbols",
    ago: "5h",
    unread: true,
    body: `Professor Langdon,

Could I come by on Wednesday? I have a draft of the chapter on the tauroctony and three questions
about the zodiac ring.

Mara`,
  },
  {
    key: "thesis-reply",
    folder: "Sent",
    from: `${owner} <${harvard}>`,
    to: mara,
    subject: "Re: Office hours: thesis on Mithraic symbols",
    ago: "3h",
    replyTo: "thesis",
    body: `Wednesday at 14:00 works. Bring the draft on paper; I read symbols better that way.

R.L.`,
  },
  {
    key: "extension",
    folder: "Students",
    from: theo,
    subject: "Extension request",
    ago: "1d",
    body: `Dear Professor,

My paper on the vesica piscis needs one more week: the photographs from Chartres arrive on
Monday. Would that be possible?

Theo`,
  },
  {
    key: "symposium",
    folder: "Committee",
    from: dean,
    subject: "Symposium programme, draft 2",
    ago: "2d",
    flagged: true,
    body: `The second draft of the programme is attached. Your keynote is on the Saturday morning.`,
    attachments: [
      { name: "symposium-programme.pdf", type: "application/pdf", size: 64000 },
    ],
  },
  {
    key: "hold",
    folder: "INBOX",
    from: library,
    subject: "Your hold is ready: De Divina Proportione",
    ago: "3d",
    body: `The item you asked for is waiting at the circulation desk until Friday.`,
  },
  {
    key: "password",
    folder: "INBOX",
    from: it,
    subject: "Your password expires in 7 days",
    ago: "4d",
    unread: true,
    body: `Choose a new password before it expires to keep your access to email and the library.`,
  },
  {
    key: "grades",
    folder: "Archive",
    from: dean,
    subject: "Grades submitted",
    ago: "3w",
    body: `Thank you: the grades for Symbology 101 are in.`,
  },
  {
    key: "sabbatical",
    folder: "Archive",
    from: dean,
    subject: "Sabbatical paperwork",
    ago: "5w",
    body: `The forms for next year's sabbatical are attached.`,
    attachments: [
      { name: "sabbatical-form.pdf", type: "application/pdf", size: 41000 },
    ],
  },
  {
    key: "cruise",
    folder: "Junk",
    from: "Prize Desk <win@cruise-prize.example>",
    subject: "You have won a cruise!!!",
    ago: "2d",
    unread: true,
    body: `Claim your free cruise now. Offer ends tonight.`,
  },
];

const publishingMail = [
  {
    key: "ending",
    folder: "INBOX",
    from: faukman,
    subject: "The ending",
    ago: "6h",
    unread: true,
    body: `Robert,

Marketing has read both endings and likes the second. So do I. Can we talk on Monday?

Jonas`,
  },
  {
    key: "ending-reply",
    folder: "Sent",
    from: `${owner} <${books}>`,
    to: faukman,
    subject: "Re: The ending",
    ago: "4h",
    replyTo: "ending",
    body: `Monday is fine. I like the second one too, which worries me.

Robert`,
  },
  {
    key: "covers",
    folder: "INBOX",
    from: marketing,
    subject: "Cover options",
    ago: "1d",
    body: `Three covers attached. The spiral is hidden in the second; see if you can find it.`,
    attachments: [
      { name: "cover-a.jpg", type: "image/jpeg", size: 90000 },
      { name: "cover-b.jpg", type: "image/jpeg", size: 88000 },
      { name: "cover-c.jpg", type: "image/jpeg", size: 93000 },
    ],
  },
  {
    key: "covers-draft",
    folder: "Drafts",
    from: `${owner} <${books}>`,
    to: marketing,
    subject: "Re: Cover options",
    ago: "20h",
    replyTo: "covers",
    body: `Found it. B, but lose the gold.`,
  },
  {
    key: "italian",
    folder: "Contracts",
    from: rights,
    subject: "Italian edition: offer",
    ago: "3d",
    flagged: true,
    body: `An Italian publisher has made an offer for the translation rights. Details attached.`,
    attachments: [
      { name: "offer-it.pdf", type: "application/pdf", size: 52000 },
    ],
  },
  {
    key: "tour",
    folder: "Tour",
    from: tour,
    subject: "Book tour: Bilbao and Paris",
    ago: "4d",
    body: `Bilbao on the 12th, Paris on the 15th. Hotels are booked; the itinerary follows.`,
  },
  {
    key: "royalties",
    folder: "Archive",
    from: "Royalties <royalties@faukman.example>",
    subject: "Royalty statement, first half",
    ago: "2w",
    body: `Your statement for January to June is attached.`,
    attachments: [
      { name: "royalties-h1.pdf", type: "application/pdf", size: 38000 },
    ],
  },
];

// Weekly issues of a few newsletters, for months: enough to move a great many at once.
const letters = [
  ["Signs & Symbols Weekly", "news@signs.example", "This week in symbols"],
  ["The Cipher Post", "editor@cipher.example", "Puzzles and codes"],
  ["Museum Digest", "digest@museum.example", "Exhibitions to see"],
  ["Paris Notes", "hello@parisnotes.example", "A week in Paris"],
  ["Architecture Today", "letters@arch.example", "Buildings worth a look"],
  ["Old Maps Monthly", "maps@oldmaps.example", "Charts and atlases"],
  ["Rare Books Bulletin", "bulletin@rarebooks.example", "New at auction"],
  ["Travel Light", "team@travellight.example", "Packing for one"],
];
const newsletterMail = [];
for (let week = 0; week < 20; week++) {
  letters.forEach(([name, address, topic], i) => {
    const days = week * 7 + i;
    newsletterMail.push({
      key: `letter-${week}-${i}`,
      folder: week > 12 && i % 2 === 0 ? "Reading list" : "INBOX",
      from: `${name} <${address}>`,
      subject: `${topic}, issue ${120 - week}`,
      ago: days === 0 ? "3h" : `${days}d`,
      unread: week < 2,
      unsubscribe: `https://${address.split("@")[1]}/unsubscribe/demo`,
      body: `${topic}: this week's picks, a long read, and three things to see.

You get this because you subscribed. Unsubscribe any time.`,
      html: `<h2>${topic}</h2><p>This week's picks, a long read, and three things to see.</p>`,
    });
  });
}

// Two years of an address that is not used much any more.
const oldSenders = [
  "Sophie Neveu <sophie@neveu.example>",
  "Leigh Teabing <leigh@teabing.example>",
  "Peter Solomon <peter@solomon.example>",
  "Vittoria Vetra <vittoria@cern.example>",
  "Ambra Vidal <ambra@guggenheim.example>",
];
const oldTopics = [
  "Lunch next week?",
  "The manuscript you asked about",
  "Photos from the trip",
  "A question about a seal",
  "Conference in Rome",
  "Your lecture notes",
  "Dinner at the club",
  "That book I mentioned",
];
const oldMail = [];
for (let i = 0; i < 80; i++) {
  oldMail.push({
    key: `old-${i}`,
    folder: i < 6 ? "INBOX" : "Archive",
    from: oldSenders[i % oldSenders.length],
    subject: oldTopics[i % oldTopics.length],
    ago: `${2 + i * 9}d`,
    unread: i < 2,
    body: `Robert,

${oldTopics[i % oldTopics.length]}. Write when you have a moment.`,
  });
}

export const accounts = [
  {
    name: "Personal",
    email: "langdon@example.com",
    folders: personalFolders,
    messages: personalMessages,
  },
  {
    name: "Harvard",
    email: harvard,
    folders: [...own, "Students", "Committee"],
    messages: harvardMail,
  },
  {
    name: "Publishing",
    email: books,
    folders: [...own, "Contracts", "Tour"],
    messages: publishingMail,
  },
  {
    name: "Newsletters",
    email: "subscriptions@example.com",
    folders: [...own, "Reading list"],
    messages: newsletterMail,
  },
  {
    name: "Old address",
    email: "rlangdon@oldmail.example",
    folders: own,
    messages: oldMail,
  },
];
