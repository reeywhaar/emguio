# Conventions

Words, commits, comments, ids, time. Settled once here so it is not re-argued in review.

## Words

One name per thing, in identifiers, the API, the docs and the interface.

| word | means | in code |
| --- | --- | --- |
| user | a person who signs in to emguio | `User`, `users`, `user_id` |
| email config | one of a user's mail accounts: an incoming server and an optional outgoing one | `EmailConfig`, `email_configs`, `email_config_id` |
| mailbox | a folder on the incoming server | `Mailbox`, `mailboxes`, `mailbox_id` |
| message | one email | `Message`, `messages`, `message_id` |
| part | one MIME part of a message: a body or an attachment | `Part`, `parts` |

**"Account" is not a word here.** To one reader it means the emguio login and to the next the
mail account, and the code that confuses them leaks one user's mail into another's view. A
provider's account — "a Gmail account" — is fine in prose about providers.

**The interface says "folder"; the code says "mailbox".** Mailbox is the protocol's word, and
the code is written against the protocol. Folder is the word a person reads.

## American spelling

`color`, `favorite`, `theater`. Not `colour`, `favourite`, `theatre`.

Not a view about which is the better English. It is the dialect computers speak, and everything
this is written against already spells it that way — CSS has `color` and `background-color`,
the DOM has `colorScheme`, JSON field names follow the Go struct fields, and those follow the
CSS. A codebase that says `colour` in its own names has two spellings of one word and a rule
nobody can remember about which half is which.

It is one word in one dialect, everywhere: identifiers, comments, commit messages, the interface
itself.

## Comments

Commit messages are one line, no co-authored

**Why, never what.** A comment restating the line under it is noise. A comment recording the
reason a line is written the way it is stops somebody "simplifying" it back into a bug.

**No prose.** A comment is a note, not a paragraph. Say the reason in a sentence or two and
stop. If it needs three paragraphs to justify, that argument belongs in `docs/` and the comment
points at it.

**No history.** Never "this used to be X", "an earlier version did Y", "this was changed
because". Git holds that. A comment describes the code as it is, in the present tense, to
somebody who has never seen any other version of it.

```go
// Good
// EXAMINE, not SELECT: a read-only session cannot change \Seen by accident.

// Bad — history
// This used SELECT before, but opening a message marked it read on the server, so now
// it uses EXAMINE.

// Bad — prose
// We open the mailbox with EXAMINE rather than SELECT because EXAMINE gives us a
// read-only session, and in a read-only session the server will not set the \Seen flag
// even if some later fetch forgets to use BODY.PEEK, which protects the user's unread
// state on every other client they use.

// Bad — what
// Open the mailbox read-only.
```

**No negations.** A comment explains the code that is there. It does not explain what is
*absent* — why a dependency was not taken, why a feature does not exist, why a directive was
left out. Those are decisions, they belong in `docs/`, and a comment is the wrong shape for
them: it sits beside code that has nothing to do with the thing being argued about, it is
invisible to anybody who did not already open that file, and there is no natural place for the
second one when a related question comes up.

```go
// Bad — an argument about something that is not here
// No IDLE yet. Two servers we tried dropped idle connections after a minute, so we
// poll until that is understood.

// Good — the decision in docs/, and nothing beside the code
```

If a reader would be surprised by an absence, that surprise belongs in the document that
covers the subject, where it can be found by somebody asking the question rather than only by
somebody reading that file.

**Say it once.** The same reason in a function, its test and its caller is three copies to keep
true, and the two that fall behind are the ones somebody will read.

**Package doc comments are expected**, and are the right place for the argument a package
exists to make — not what it contains, which is readable.

## Docs

`docs/` holds the decisions, one file per subject. A heading states the decision and the text
under it gives the reason:

```md
## A mailbox is opened with EXAMINE
```

Not `## Opening mailboxes`. A reader scanning the headings learns what was decided without
opening a section, and a section that cannot be headed by a sentence has not decided anything
yet.

A decision is written down in the change that makes it, beside the code — not afterwards, and
not in a commit message, which nobody reads at the moment they need it.

## Examples

**The smallest thing that works, and nothing else.** A `docker run` line, a compose file, a
`curl` — each shows only what a reader must supply. Log levels, tuning knobs, anything that
exists for a checkout rather than a deployment: those belong in the reference table beside the
example, never in it.

An example carrying a variable nobody needs teaches that it is required, and it is copied into
every deployment that follows.

## Commits

**One or two lines.** A declarative sentence saying what the change accomplishes. Capitalized,
no trailing period, no prefix, no conventional-commit tag, no ticket number.

**No trailers. No `Co-Authored-By`. No generated-with footer.** Ever, whoever or whatever wrote
the change.

```
An email config's password is sealed with the server key before it is stored

Open a mailbox with EXAMINE
so reading in emguio never marks a message read on the server
```

A second line is for the clause that would not fit, not for a body. A change that genuinely
needs three paragraphs is usually two changes.

## Go

- `gofmt` clean; CI fails on anything it would rewrite.
- Tests beside sources as `*_test.go`. No `tests/` directory.
- Errors wrap with `%w` and name what was being done.
- One error vocabulary in `internal/store` — `ErrNotFound`, `ErrConflict`, `ErrInvalid` — built
  through `store.NotFound`, `store.Conflict` and `store.Invalid` so the message is a sentence
  written for whoever reads it.
- `context.Context` first parameter on anything that can block. Everything that talks to a mail
  server can block.
- Injectable clocks, so expiry is driven rather than slept through.
- A function returning "this succeeded, and also something happened" returns a bool, not a
  sentinel error.
- Test names are sentences: `TestAMailboxIsDroppedWhenItsUIDValidityChanges`.

## TypeScript

- Prettier, default settings.
- Components are `PascalCase.tsx`; everything else is `camelCase.ts`.
- Imports use the `@app/*` alias, never `../../`.
- `strict`, `noUncheckedIndexedAccess`, `noUnusedLocals`, `noUnusedParameters`,
  `verbatimModuleSyntax`.

## Mail is hostile input

Everything a mail server sends was written by a stranger.

- **Text is decoded where it arrives.** Header words and bodies go through their declared
  charset, then `strings.ToValidUTF8`, inside the package that fetched them. `encoding/json`
  refuses invalid UTF-8, and a header with one stray byte must not take the list down with it.
- **HTML from a message never enters the app's document.** It is sanitized on the server and
  rendered only inside the sandboxed message frame. No `dangerouslySetInnerHTML` with mail in
  it, anywhere.
- **A host typed into an email config is dialed through the screened dialer**, never through a
  bare `net.Dial`.

## Identifiers

Three kinds, and the rule that decides between them:

> A software-only identifier is a ULID. An identifier a person or a model types is short. An
> identifier the caller has to compute without asking is derived.

| kind | shape | used by |
| --- | --- | --- |
| ULID | prefix plus 26 Crockford characters over 16 bytes | `u_` user, `i_` invite, `ec_` email config, `mb_` mailbox, `j_` job, `d_` draft, `dp_` draft part |
| derived | hash of the thing it names | tokens |
| the server's | `{uidvalidity}-{uid}`, under its mailbox's id | messages |

A new prefix is added to this table in the change that introduces it.

Ids are opaque and never parsed back. A malformed one is refused before it reaches a query, so a
typo is a `400` rather than an empty result that looks like a `404`.

**What the server calls a thing is not an id**, except a message's. Mailbox names stay inside
`internal/store` and the package that speaks the protocol, and the API, URLs and the interface
use our own ids, so a server that renames a mailbox changes a row rather than links.

A message is the exception because most messages have no row to name them by: only INBOX's
newest are kept. Its id is its mailbox's UIDVALIDITY and its UID, which IMAP allows only as
positive 32-bit numbers, and which a renumbering server changes — so an old link is `gone`,
never another message. See [reading.md](reading.md).

## Migrations

Named `<timestamp>_<snake_case_name>.go`, where the timestamp is the UTC moment the file was
written:

```sh
date -u +%Y%m%d%H%M%S
```

Not a counter. Two people working at once pick the same next integer and do not pick the same
second. The runner sorts by name, so the timestamp is the order.

**Never edit a released migration.** Every deployment past it has recorded it as applied and
will skip the edit forever.

## Time

- `main.go` pins `time.Local = time.UTC`. Everything stored and logged is UTC.
- Stored as **Unix seconds in an `INTEGER` column**. Not text, not milliseconds. A message's
  internal date and its `Date:` header are converted on the way in.
- The server never formats a date for a person. The browser shows it in the viewer's zone.

## Logging

`log/slog`. Log what an operator needs: a login refused, a user created, a token refused and
why, an email config that cannot connect and the class of error, a sync that dropped a mailbox, a
sweep that deleted something, a backup sent or failed, a migration applied.

**Never** a password — a user's or an email config's — the server key, a token, a cookie value,
a recovery code. **Never** anything from a message: subject, addresses, body, attachment names.
A log line about an email config names its id and host; one about a message names its id.

`debug` is a level, not an exemption.
