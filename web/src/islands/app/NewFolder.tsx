import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { postEmailConfigsByIdMailboxes } from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Mailbox } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { Field, Select } from "@app/components/Field";
import { TextField } from "@app/components/TextField";
import { depthOf, labelOfMailbox } from "@app/islands/app/mailbox";

/** A folder made on the mail server, at the top or inside another, for a move to go to. */
export function NewFolder({
  config,
  boxes,
  open,
  onClose,
  onMade,
}: {
  config: string;
  boxes: Mailbox[];
  open: boolean;
  onClose: () => void;
  onMade: (made: Mailbox) => void;
}) {
  const client = useQueryClient();
  const form = useId();
  const [name, setName] = useState("");
  const [parent, setParent] = useState("");
  const make = useMutation({
    mutationFn: () =>
      postEmailConfigsByIdMailboxes(config, { name: name.trim(), parent }),
    onSuccess: (made) => {
      // There for the move at once; the sidebar's order follows from the next read.
      client.setQueryData<Mailbox[]>(
        qk.mailboxes(config),
        (old) => old && [...old, made],
      );
      void client.invalidateQueries({ queryKey: qk.mailboxes(config) });
      onMade(made);
    },
  });

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Move to a new folder"
      footer={
        <>
          {make.error ? (
            <p role="alert" className="w-full text-sm text-accent">
              {messageOf(make.error)}
            </p>
          ) : null}
          <Button onClick={onClose}>Cancel</Button>
          <Button
            type="submit"
            form={form}
            variant="solid"
            disabled={make.isPending || name.trim() === ""}
          >
            {make.isPending ? "Making" : "Make and move"}
          </Button>
        </>
      }
    >
      <form
        id={form}
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          make.mutate();
        }}
      >
        <Field label="Name">
          <TextField
            value={name}
            onChange={(e) => setName(e.target.value)}
            data-autofocus
          />
        </Field>
        <Field label="Inside">
          <Select value={parent} onChange={(e) => setParent(e.target.value)}>
            <option value="">No folder, at the top</option>
            {boxes.map((mb) => (
              <option key={mb.id} value={mb.id}>
                {" ".repeat(depthOf(mb))}
                {labelOfMailbox(mb)}
              </option>
            ))}
          </Select>
        </Field>
      </form>
    </Dialog>
  );
}
