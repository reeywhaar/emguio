import { useId, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { postAuthLogout } from "@app/api/actions/auth";
import {
  postSeal,
  postSealCarry,
  postSealReset,
  postSealUnseal,
  putSeal,
} from "@app/api/actions/seal";
import { messageOf } from "@app/api/transport";
import type { Me } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { NewSealKey } from "@app/components/NewSealKey";
import { TextField } from "@app/components/TextField";
import { say } from "@app/islands/app/notices";
import { leaveFor } from "@app/leave";
import { forget } from "@app/push";
import { newKeyProblem } from "@app/sealKey";

/**
 * What a signed-in user sees until their seal key is open: unsealing after a restart, making a
 * new key for a forgotten one, or a first one for a user from before seal keys. Nothing else of
 * theirs can be read until then. See docs/email-configs.md.
 */
export function Seal({ state }: { state: Exclude<Me["seal"], "open"> }) {
  // For a user who has none yet: which way they make it, or not chosen.
  const [way, setWay] = useState<"carry" | "make" | null>(null);
  const [forgot, setForgot] = useState(false);
  const back = () => setWay(null);
  return (
    <main className="mx-auto flex w-full max-w-sm flex-1 flex-col justify-center gap-6 px-4 py-8">
      {state === "sealed" ? (
        forgot ? (
          <NewKey mode="reset" onBack={() => setForgot(false)} />
        ) : (
          <Unseal onForgot={() => setForgot(true)} />
        )
      ) : way === "carry" ? (
        <Carry onBack={back} />
      ) : way === "make" ? (
        <NewKey mode="make" onBack={back} />
      ) : (
        <Choose onChoose={setWay} />
      )}
      <SignOut />
    </main>
  );
}

/** For a user from before seal keys: keep the passwords with the key emguio ran with, or not. */
function Choose({ onChoose }: { onChoose: (way: "carry" | "make") => void }) {
  return (
    <>
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold">Your seal key</h1>
        <p className="text-sm text-muted">
          emguio now seals your mail passwords with a key of your own, and asks
          for it after every restart. The key emguio ran with until now keeps
          your saved passwords and becomes your seal key.
        </p>
      </div>
      <div className="flex flex-col gap-2">
        <Button
          variant="solid"
          className="justify-center"
          onClick={() => onChoose("carry")}
        >
          I have the current key
        </Button>
        <Button className="justify-center" onClick={() => onChoose("make")}>
          Create a seal key
        </Button>
      </div>
    </>
  );
}

/** The key emguio ran with, which opens the saved passwords and is the seal key from now on. */
function Carry({ onBack }: { onBack: () => void }) {
  const client = useQueryClient();
  const [key, setKey] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await postSealCarry(key);
      await client.invalidateQueries();
    } catch (err) {
      setError(messageOf(err));
      setBusy(false);
    }
  };
  return (
    <>
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold">Your current key</h1>
        <p className="text-sm text-muted">
          The key emguio ran with, set as EMGUIO_SECRET_KEY. Your saved mail
          passwords are kept, and it becomes your seal key; you can change it in
          Settings.
        </p>
      </div>
      <form className="flex flex-col gap-3" onSubmit={submit}>
        <TextField
          name="current-key"
          type="password"
          autoComplete="off"
          aria-label="Current key"
          placeholder="Current key"
          className="w-full"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          autoFocus
        />
        {error ? <p className="text-sm text-accent">{error}</p> : null}
        <Button
          type="submit"
          variant="solid"
          className="justify-center"
          disabled={busy || key === ""}
        >
          {busy ? "Keeping them" : "Keep my passwords"}
        </Button>
      </form>
      <BackLink onBack={onBack} />
    </>
  );
}

function BackLink({ onBack }: { onBack: () => void }) {
  return (
    <button
      type="button"
      className="self-start text-sm text-muted underline underline-offset-2 hover:text-fg"
      onClick={onBack}
    >
      Back
    </button>
  );
}

function Unseal({ onForgot }: { onForgot: () => void }) {
  const client = useQueryClient();
  const [key, setKey] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await postSealUnseal(key);
      await client.invalidateQueries();
    } catch (err) {
      setError(messageOf(err));
      setBusy(false);
    }
  };
  return (
    <>
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold">Unseal</h1>
        <p className="text-sm text-muted">
          emguio has restarted. Enter your seal key to open your mail accounts.
        </p>
      </div>
      <form className="flex flex-col gap-3" onSubmit={submit}>
        <TextField
          name="seal-key"
          type="password"
          autoComplete="off"
          aria-label="Seal key"
          placeholder="Seal key"
          className="w-full"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          autoFocus
        />
        {error ? <p className="text-sm text-accent">{error}</p> : null}
        <Button
          type="submit"
          variant="solid"
          className="justify-center"
          disabled={busy || key === ""}
        >
          {busy ? "Unsealing" : "Unseal"}
        </Button>
      </form>
      <button
        type="button"
        className="self-start text-sm text-muted underline underline-offset-2 hover:text-fg"
        onClick={onForgot}
      >
        Forgot your seal key?
      </button>
    </>
  );
}

/** A new seal key: a first one, or one in place of a forgotten one. */
function NewKey({
  mode,
  onBack,
}: {
  mode: "make" | "reset";
  onBack?: () => void;
}) {
  const client = useQueryClient();
  const [key, setKey] = useState("");
  const [again, setAgain] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const problem = newKeyProblem(key, again);
    if (problem) {
      setError(problem);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await (mode === "make" ? postSeal(key) : postSealReset(key));
      await client.invalidateQueries();
    } catch (err) {
      setError(messageOf(err));
      setBusy(false);
    }
  };
  return (
    <>
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold">
          {mode === "make" ? "Create a seal key" : "Set a new seal key"}
        </h1>
        <p className="text-sm text-muted">
          {mode === "make"
            ? "emguio asks for it after every restart. Each mail account asks for its password once more."
            : "A new seal key opens emguio. Each mail account then asks for its password again; its folders and settings are kept."}
        </p>
      </div>
      <form className="flex flex-col gap-3" onSubmit={submit}>
        <NewSealKey
          label="New seal key"
          name="new-seal-key"
          value={key}
          again={again}
          onChange={(next, nextAgain) => {
            setKey(next);
            setAgain(nextAgain);
          }}
          autoFocus
        />
        {error ? <p className="text-sm text-accent">{error}</p> : null}
        <Button
          type="submit"
          variant={mode === "reset" ? "danger" : "solid"}
          className="justify-center"
          disabled={busy}
        >
          {mode === "make" ? "Make it" : "Set the new key"}
        </Button>
      </form>
      {onBack ? <BackLink onBack={onBack} /> : null}
    </>
  );
}

function SignOut() {
  return (
    <button
      type="button"
      className="self-start text-sm text-muted underline underline-offset-2 hover:text-fg"
      onClick={async () => {
        await forget();
        await postAuthLogout();
        leaveFor("/login");
      }}
    >
      Sign out
    </button>
  );
}

/** The seal key in Settings: what it is for, in a line, and a dialog to change it. */
export function SealKeySettings() {
  const [opened, setOpened] = useState(0);
  const [open, setOpen] = useState(false);
  return (
    <section className="flex flex-col gap-3">
      <h2 className="caps text-muted">Seal key</h2>
      <div className="flex items-center justify-between gap-3 rounded-lg border border-line bg-bg px-4 py-2">
        <p className="min-w-0 text-sm">
          Opens your saved mail passwords after every restart
        </p>
        <Button
          variant="link"
          className="-mr-2 shrink-0 rounded-md px-2 py-1.5"
          aria-haspopup="dialog"
          onClick={() => {
            setOpened((n) => n + 1);
            setOpen(true);
          }}
        >
          Change
        </Button>
        {opened > 0 ? (
          <ChangeSealKey
            key={opened}
            open={open}
            onClose={() => setOpen(false)}
          />
        ) : null}
      </div>
    </section>
  );
}

/** The current seal key and a new one twice; the mail passwords stay as they are. */
function ChangeSealKey({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const form = useId();
  const [current, setCurrent] = useState("");
  const [key, setKey] = useState("");
  const [again, setAgain] = useState("");
  const [problem, setProblem] = useState("");
  const change = useMutation({
    mutationFn: () => putSeal(current, key),
    onSuccess: () => {
      say("Your seal key is changed.");
      onClose();
    },
  });
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Change seal key"
      footer={
        <>
          {problem || change.error ? (
            <p role="alert" className="w-full text-sm text-accent">
              {problem || messageOf(change.error)}
            </p>
          ) : null}
          <Button onClick={onClose}>Cancel</Button>
          <Button
            type="submit"
            form={form}
            variant="solid"
            disabled={change.isPending}
          >
            {change.isPending ? "Saving" : "Save"}
          </Button>
        </>
      }
    >
      <form
        id={form}
        aria-label="Change seal key"
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          const why =
            current === ""
              ? "Enter your current seal key."
              : newKeyProblem(key, again);
          setProblem(why);
          if (!why) change.mutate();
        }}
      >
        <TextField
          name="seal-key"
          type="password"
          autoComplete="off"
          aria-label="Current seal key"
          placeholder="Current seal key"
          className="w-full"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          autoFocus
        />
        <NewSealKey
          label="New seal key"
          name="new-seal-key"
          value={key}
          again={again}
          onChange={(next, nextAgain) => {
            setKey(next);
            setAgain(nextAgain);
          }}
        />
        <p className="text-xs text-muted">
          Your saved mail passwords stay as they are.
        </p>
      </form>
    </Dialog>
  );
}
