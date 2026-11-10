import { useMutation, useQueryClient } from "@tanstack/react-query";

import { postEmailConfigsByIdMailboxesByMailboxEmpty } from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Mailbox } from "@app/api/types";
import { Button } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import { labelOfMailbox } from "@app/islands/app/mailbox";
import { say } from "@app/islands/app/notices";

/** Whether a folder is one emptied from under its search field: Trash and Junk. */
export const emptied = (mb: Mailbox) =>
  mb.special_use === "trash" || mb.special_use === "junk";

/** Trash or Junk emptied for good, asked about first. See docs/reading.md. */
export function EmptyFolder({
  config,
  mailbox,
}: {
  config: string;
  mailbox: Mailbox;
}) {
  const client = useQueryClient();
  const confirm = useConfirm();
  const name = labelOfMailbox(mailbox);
  const count = `${mailbox.messages.toLocaleString()} ${mailbox.messages === 1 ? "message" : "messages"}`;
  const empty = useMutation({
    mutationFn: () =>
      postEmailConfigsByIdMailboxesByMailboxEmpty(config, mailbox.id),
    onSettled: () => {
      void client.invalidateQueries({
        queryKey: qk.folder(config, mailbox.id),
      });
      void client.invalidateQueries({ queryKey: qk.mailboxes(config) });
    },
    onError: (err) => say(messageOf(err)),
  });
  return (
    <div className="flex items-center justify-between gap-3 border-b border-line bg-bg px-4 py-2">
      <span className="min-w-0 text-sm text-muted">{count}</span>
      <Button
        size="bar"
        variant="danger"
        className="shrink-0"
        disabled={empty.isPending}
        onClick={async () => {
          if (
            await confirm({
              title: `Empty ${name}?`,
              message: `All ${count} in ${name} are deleted from the server. This cannot be undone.`,
              confirm: `Empty ${name}`,
              danger: true,
            })
          ) {
            empty.mutate();
          }
        }}
      >
        {empty.isPending ? "Emptying" : `Empty ${name}…`}
      </Button>
    </div>
  );
}
