import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { putAuthPassword } from "@app/api/actions/auth";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { TextField } from "@app/components/TextField";
import { say } from "@app/islands/app/notices";

/** What the server refuses anything shorter than. */
const PASSWORD_MIN = 8;

/**
 * The current password and a new one twice. The current one is asked for because being signed in
 * is not knowing it: that is what keeps a borrowed session from becoming a taken account. Every
 * other browser is signed out; this one stays.
 */
export function PasswordDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const form = useId();
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [problem, setProblem] = useState("");
  const change = useMutation({
    mutationFn: () => putAuthPassword({ current, password }),
    onSuccess: () => {
      // Nothing typed stays on the screen.
      setCurrent("");
      setPassword("");
      setAgain("");
      void client.invalidateQueries({ queryKey: qk.sessions });
      say("Your password is changed. Every other browser is signed out.");
      onClose();
    },
  });
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Change password"
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
        aria-label="Change password"
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          const why =
            current === ""
              ? "Enter your current password."
              : password.length < PASSWORD_MIN
                ? `A password needs at least ${PASSWORD_MIN} characters.`
                : password !== again
                  ? "The two new passwords do not match."
                  : "";
          setProblem(why);
          if (!why) change.mutate();
        }}
      >
        <TextField
          name="current-password"
          type="password"
          autoComplete="current-password"
          aria-label="Current password"
          placeholder="Current password"
          className="w-full"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          autoFocus
        />
        <TextField
          name="new-password"
          type="password"
          autoComplete="new-password"
          aria-label="New password"
          placeholder="New password"
          className="w-full"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <TextField
          name="new-password-again"
          type="password"
          autoComplete="new-password"
          aria-label="New password again"
          placeholder="New password again"
          className="w-full"
          value={again}
          onChange={(e) => setAgain(e.target.value)}
        />
        <p className="text-xs text-muted">
          At least {PASSWORD_MIN} characters. Every other browser is signed out;
          this one stays.
        </p>
      </form>
    </Dialog>
  );
}
