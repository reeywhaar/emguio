# Demo

emguio with a mail server of its own and five mail accounts full of invented mail, in Docker.
The mail is made fresh on every start. emguio's user, its sign-in and its accounts are kept from
one run to the next, so a browser signed in stays signed in.

```sh
./run_demo.sh
```

from the top of the checkout. It builds and starts everything, says when the accounts are filled,
and follows emguio's log; then open <http://localhost:8080> and sign in as **demo**, password
**demodemo**. The seal key is **demodemodemo**; each run opens it for you, so it is asked for only
if emguio restarts on its own. Ctrl-C stops it. `./run_demo.sh --fresh` starts without what earlier runs kept,
signed out. To serve on another port, set `DEMO_PORT=9090`.

Without the script: `docker compose up --build` here, and `docker compose down` to stop, with
`--volumes` to forget what runs kept. `docker compose logs seed` says when the accounts are filled.

## What is in it

The mail of Robert Langdon, who has five accounts:

| account | address | |
| --- | --- | --- |
| Personal | langdon@example.com | The mail the README's screenshots show. |
| Harvard | langdon@harvard.example | Students, a committee, the library. |
| Publishing | robert@langdon-books.example | An editor, covers, contracts, a book tour. |
| Newsletters | subscriptions@example.com | 160 weekly issues of eight newsletters, to unsubscribe from and move in bulk. |
| Old address | rlangdon@oldmail.example | Two years of archive. |

## What to try

- Write from one account to another: mail to any of the five addresses arrives in its Inbox.
  Mail to anybody else goes nowhere.
- Move mail to another account: select some newsletters, choose "Another account…" under
  "Move to…", and pick a folder there. The card in the corner shows it going.
- Add a sixth account in Settings with any address on `example.com` and the password
  `demodemo`: the servers fill in on their own.

## How it is put together

- `mail`, a small mail server in [mailserver/](mailserver/): IMAP and SMTP over TLS, and the
  settings a provider publishes, for any address with the password `demodemo`, held in memory.
  It makes a certificate authority on start, which emguio is told to trust, and numbers its
  mailboxes afresh, so emguio reads them again rather than taking them for the last run's.
- `emguio`, built from this checkout, its data in the `data` volume.
- `seed`, which fills the accounts from [accounts.mjs](accounts.mjs), signs up the demo user
  through the invitation emguio prints on its first start, adds the five accounts or has emguio
  read the ones it kept again, and stops.

The mail server knows no other server: nothing is sent out, and sending later is not offered.
