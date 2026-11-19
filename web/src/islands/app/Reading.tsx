import { useId, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { putEmailConfigsByIdReading } from "@app/api/actions/emailConfigs";
import { getReading, putReading } from "@app/api/actions/reading";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { EmailConfig, Reading as Settings } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Choice } from "@app/components/Choice";
import { Dialog } from "@app/components/Dialog";
import { Dummy } from "@app/components/Dummy";
import { Select } from "@app/components/Field";
import { Switch } from "@app/components/Switch";

/** What archiving and deleting do to unread mail, in a few words. */
function summaryOf(r: Settings): string {
  if (r.read_on_archive && r.read_on_trash) {
    return "Archived and deleted mail is marked read";
  }
  if (r.read_on_archive) return "Archived mail is marked read";
  if (r.read_on_trash) return "Deleted mail is marked read";
  return "Archiving and deleting leave mail as it is";
}

/**
 * Whether unread mail is marked read as it is archived, and as it is deleted, for every mail
 * account that does not choose for itself: in a line, and chosen in a dialog. See
 * docs/reading.md.
 */
export function Reading() {
  const [opened, setOpened] = useState(0);
  const [open, setOpen] = useState(false);
  const reading = useQuery({ queryKey: qk.reading, queryFn: getReading });
  return (
    <section className="flex flex-col gap-3">
      <h2 className="caps text-muted">Reading</h2>
      {!reading.data ? (
        reading.error ? (
          <p role="alert" className="text-sm text-accent">
            {messageOf(reading.error)}
          </p>
        ) : (
          <Dummy className="h-11 w-full" />
        )
      ) : (
        <div className="flex items-center justify-between gap-3 rounded-lg border border-line bg-bg px-4 py-2">
          <p className="min-w-0 text-sm">{summaryOf(reading.data)}</p>
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
            <ReadingDialog
              key={opened}
              reading={reading.data}
              open={open}
              onClose={() => setOpen(false)}
            />
          ) : null}
        </div>
      )}
    </section>
  );
}

/** Both choices, changed together and saved at once. */
function ReadingDialog({
  reading,
  open,
  onClose,
}: {
  reading: Settings;
  open: boolean;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const form = useId();
  const [draft, setDraft] = useState(reading);
  const save = useMutation({
    mutationFn: () => putReading(draft),
    onSuccess: (saved) => {
      client.setQueryData(qk.reading, saved);
      onClose();
    },
  });
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Reading"
      footer={
        <>
          {save.error ? (
            <p role="alert" className="w-full text-sm text-accent">
              {messageOf(save.error)}
            </p>
          ) : null}
          <Button onClick={onClose}>Cancel</Button>
          <Button
            type="submit"
            form={form}
            variant="solid"
            disabled={save.isPending}
          >
            {save.isPending ? "Saving" : "Save"}
          </Button>
        </>
      }
    >
      <form
        id={form}
        aria-label="Reading"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Choice
          title="Mark read when archiving"
          note="Unread mail you archive is marked read first."
        >
          <Switch
            on={draft.read_on_archive}
            label="Mark read when archiving"
            onChange={(on) => setDraft({ ...draft, read_on_archive: on })}
          />
        </Choice>
        <Choice
          title="Mark read when deleting"
          note="Unread mail you move to Trash is marked read first."
        >
          <Switch
            on={draft.read_on_trash}
            label="Mark read when deleting"
            onChange={(on) => setDraft({ ...draft, read_on_trash: on })}
          />
        </Choice>
        <p className="text-xs text-muted">
          A mail account can choose otherwise on its card.
        </p>
      </form>
    </Dialog>
  );
}

const choiceOf = (v: boolean | null) =>
  v === null ? "" : v ? "read" : "leave";
const valueOf = (choice: string) => (choice === "" ? null : choice === "read");

/**
 * Whether one config's mail is marked read as it is archived, or as it is deleted: as set for
 * all, or its own. Saved as it is chosen.
 */
export function ReadOn({
  config,
  which,
}: {
  config: EmailConfig;
  which: "read_on_archive" | "read_on_trash";
}) {
  const client = useQueryClient();
  const reading = useQuery({ queryKey: qk.reading, queryFn: getReading });
  const choose = useMutation({
    mutationFn: (choice: string) =>
      putEmailConfigsByIdReading(config.id, {
        read_on_archive: config.read_on_archive,
        read_on_trash: config.read_on_trash,
        [which]: valueOf(choice),
      }),
    onSuccess: (saved) =>
      client.setQueryData<EmailConfig[]>(qk.emailConfigs, (old) =>
        old?.map((c) => (c.id === saved.id ? saved : c)),
      ),
  });
  const all = reading.data?.[which];
  return (
    <div className="flex flex-col gap-1">
      <Select
        size="bar"
        aria-label={
          which === "read_on_archive" ? "When archived" : "When deleted"
        }
        className="max-w-full sm:max-w-72"
        value={choose.isPending ? choose.variables : choiceOf(config[which])}
        disabled={choose.isPending}
        onChange={(e) => choose.mutate(e.target.value)}
      >
        <option value="">
          {all === undefined
            ? "As set for all"
            : `As set for all: ${all ? "mark read" : "leave as it is"}`}
        </option>
        <option value="read">Mark read</option>
        <option value="leave">Leave as it is</option>
      </Select>
      {choose.error ? (
        <p className="text-sm text-accent">{messageOf(choose.error)}</p>
      ) : null}
    </div>
  );
}
