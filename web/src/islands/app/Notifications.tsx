import { useId, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getEmailConfigsByIdMailboxes,
  putEmailConfigsByIdNotifications,
} from "@app/api/actions/emailConfigs";
import {
  deleteDevicesById,
  getDevices,
  postDevicesTest,
} from "@app/api/actions/push";
import { qk } from "@app/api/keys";
import { ApiError, messageOf } from "@app/api/transport";
import type { EmailConfig, Mailbox } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { Dummy } from "@app/components/Dummy";
import { Select } from "@app/components/Field";
import { WarnMark } from "@app/components/icons";
import { labelOf } from "@app/islands/app/emailConfig";
import { depthOf, labelOfMailbox } from "@app/islands/app/mailbox";
import {
  enable,
  forget,
  pushState,
  storedClientID,
  type PushState,
} from "@app/push";

/** How often, as one choice: each message as it arrives, a digest every so often, or never. */
const choices = [
  { value: "now", label: "As mail arrives" },
  { value: "30", label: "Every 30 minutes" },
  { value: "60", label: "Every hour" },
  { value: "120", label: "Every 2 hours" },
  { value: "240", label: "Every 4 hours" },
  { value: "off", label: "Never" },
];

function choiceOf(c: Pick<EmailConfig, "notify" | "digest_every">): string {
  return c.notify === "digest" ? String(c.digest_every) : c.notify;
}

/**
 * Which devices show the user's notifications, this one among them. How each mail account's new
 * mail is told is on its card. See docs/push.md.
 */
export function Notifications() {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="caps text-muted">Notifications</h2>
      <Devices />
    </section>
  );
}

/** Why a browser would not subscribe, which is not the server's sentence. */
function whyNot(err: unknown): string {
  if (err instanceof ApiError || !(err instanceof Error)) return messageOf(err);
  return `This browser would not turn them on: ${err.message}`;
}

/** Whether this device shows them, and every device that does. */
function Devices() {
  const client = useQueryClient();
  const [state, setState] = useState<PushState>(() => pushState());
  const devices = useQuery({ queryKey: qk.devices, queryFn: getDevices });
  const settled = () => client.invalidateQueries({ queryKey: qk.devices });
  const on = useMutation({
    mutationFn: enable,
    onSuccess: setState,
    onSettled: settled,
  });
  const off = useMutation({ mutationFn: forget, onSettled: settled });
  const remove = useMutation({
    mutationFn: deleteDevicesById,
    onSettled: settled,
  });
  const test = useMutation({ mutationFn: postDevicesTest });
  const mine = storedClientID();
  const here = devices.data?.find((d) => mine !== "" && d.client_id === mine);

  let sentence: string;
  let action = null;
  // This device will not show them, whatever is chosen here.
  let warn = false;
  switch (state) {
    case "unsupported":
      sentence = "This browser cannot show notifications.";
      warn = true;
      break;
    case "needs-install":
      sentence =
        "On iPhone and iPad, notifications come to emguio on the Home Screen. Tap Share, then Add to Home Screen, and turn them on from there.";
      break;
    case "denied":
      sentence =
        "Notifications are blocked for emguio here. Allow them in the browser's settings, then come back.";
      warn = true;
      break;
    default:
      sentence = here
        ? "This device shows notifications of new mail."
        : "This device does not show notifications.";
      action = here ? (
        <Button
          size="bar"
          className="shrink-0"
          disabled={off.isPending}
          onClick={() => off.mutate()}
        >
          Turn off
        </Button>
      ) : (
        <Button
          size="bar"
          className="shrink-0"
          variant="solid"
          disabled={on.isPending || !devices.data}
          onClick={() => on.mutate()}
        >
          {on.isPending ? "Turning on" : "Turn on"}
        </Button>
      );
  }

  return (
    <div className="flex flex-col gap-3 rounded-lg border border-line bg-bg p-4">
      <div className="flex items-center justify-between gap-3">
        {warn ? (
          <p className="flex min-w-0 items-start gap-2 text-sm text-warn">
            <span className="mt-0.5 shrink-0">
              <WarnMark />
            </span>
            {sentence}
          </p>
        ) : (
          <p className="min-w-0 text-sm">{sentence}</p>
        )}
        {action}
      </div>
      {on.error ? (
        <p role="alert" className="text-sm text-accent">
          {whyNot(on.error)}
        </p>
      ) : null}
      {!devices.data ? (
        <Dummy className="h-8 w-full" />
      ) : devices.data.length > 0 ? (
        <>
          <ul aria-label="Devices" className="flex flex-col gap-2 text-sm">
            {devices.data.map((d) => (
              <li
                key={d.id}
                className="flex items-center justify-between gap-3"
              >
                <span className="min-w-0">
                  {d.label || "A browser"}
                  {d === here ? (
                    <span className="text-muted"> · this device</span>
                  ) : null}
                  {d.failure_count > 0 ? (
                    <span className="block text-xs break-words text-accent">
                      Its push service refused the last one: {d.last_error}
                    </span>
                  ) : null}
                </span>
                <Button
                  size="bar"
                  className="shrink-0"
                  disabled={remove.isPending || off.isPending}
                  onClick={() =>
                    // This browser's own is let go of too, or it hands itself over again.
                    d === here ? off.mutate() : remove.mutate(d.id)
                  }
                >
                  Remove
                </Button>
              </li>
            ))}
          </ul>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              size="bar"
              className="shrink-0"
              disabled={test.isPending}
              onClick={() => test.mutate()}
            >
              Send a test
            </Button>
            {test.data ? (
              <span aria-live="polite" className="text-sm text-muted">
                {test.data.delivered === 0
                  ? "No device took it."
                  : test.data.delivered === 1
                    ? "Sent to one device."
                    : `Sent to ${test.data.delivered} devices.`}
              </span>
            ) : null}
            {test.error ? (
              <span role="alert" className="text-sm text-accent">
                {messageOf(test.error)}
              </span>
            ) : null}
          </div>
        </>
      ) : null}
    </div>
  );
}

const folders = (n: number) => (n === 1 ? "1 folder" : `${n} folders`);

/** The folders that notify, in a few words. */
function foldersOf(boxes: Mailbox[]): string {
  const on = boxes.filter((mb) => mb.selectable && mb.notify);
  const inbox = on.some((mb) => mb.special_use === "inbox");
  const others = on.length - (inbox ? 1 : 0);
  if (on.length === 0) return "no folders";
  if (!inbox) return folders(others);
  return others === 0 ? "Inbox" : `Inbox and ${folders(others)}`;
}

/** None kept while the account is unread or failing: its folders are not known, not missing. */
const unknown = (config: EmailConfig, boxes: Mailbox[]) =>
  boxes.length === 0 && (config.synced_at === null || config.sync_error !== "");

/** How an account's new mail is told, as its row says it: how often, then from which folders. */
export function notifiesOf(
  config: EmailConfig,
  boxes: Mailbox[] | undefined,
): string {
  if (config.notify === "off") return "No notifications";
  const choice = choices.find((c) => c.value === choiceOf(config));
  const how = `Notifies ${choice?.label.toLowerCase() ?? ""}`;
  return boxes && !unknown(config, boxes)
    ? `${how}, from ${foldersOf(boxes)}`
    : how;
}

/** A choice as the API takes it. */
function settingOf(choice: string) {
  const digest = choice !== "now" && choice !== "off";
  return {
    notify: digest ? ("digest" as const) : (choice as "now" | "off"),
    digest_every: digest ? Number(choice) : 0,
  };
}

/**
 * How one mail account's new mail is told: how often, saved as it is chosen, and from which
 * folders, chosen in a dialog.
 */
export function Delivery({ config }: { config: EmailConfig }) {
  const client = useQueryClient();
  const [opened, setOpened] = useState(0);
  const [open, setOpen] = useState(false);
  const boxes = useQuery({
    queryKey: qk.mailboxes(config.id),
    queryFn: () => getEmailConfigsByIdMailboxes(config.id),
  });
  const often = useMutation({
    mutationFn: (choice: string) =>
      putEmailConfigsByIdNotifications(config.id, {
        ...settingOf(choice),
        mailboxes: {},
      }),
    onSuccess: (saved) =>
      client.setQueryData<EmailConfig[]>(qk.emailConfigs, (old) =>
        old?.map((c) => (c.id === saved.id ? saved : c)),
      ),
  });
  const choice = often.isPending ? often.variables : choiceOf(config);
  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <Select
          size="bar"
          aria-label="Notify"
          value={choice}
          disabled={often.isPending}
          onChange={(e) => often.mutate(e.target.value)}
        >
          {choices.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </Select>
        {choice === "off" ? null : (
          <span>
            from{" "}
            <button
              type="button"
              aria-haspopup="dialog"
              className="underline decoration-dashed underline-offset-4 hover:decoration-solid disabled:opacity-50"
              disabled={!boxes.data && !boxes.isError}
              onClick={() => {
                setOpened((n) => n + 1);
                setOpen(true);
              }}
            >
              {boxes.data && !unknown(config, boxes.data)
                ? foldersOf(boxes.data)
                : boxes.data || boxes.isError
                  ? "folders"
                  : "…"}
            </button>
          </span>
        )}
      </div>
      {often.error ? (
        <p className="text-sm text-accent">{messageOf(often.error)}</p>
      ) : null}
      {opened > 0 ? (
        <FoldersDialog
          key={opened}
          config={config}
          boxes={boxes.data}
          failed={boxes.error}
          open={open}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </div>
  );
}

/** The folders whose new mail is told, ticked and saved together. */
function FoldersDialog({
  config,
  boxes,
  failed,
  open,
  onClose,
}: {
  config: EmailConfig;
  /** Nothing when they could not be loaded. */
  boxes: Mailbox[] | undefined;
  failed: unknown;
  open: boolean;
  onClose: () => void;
}) {
  const usable = boxes?.filter((mb) => mb.selectable) ?? [];
  const client = useQueryClient();
  const form = useId();
  const [changed, setChanged] = useState<Record<string, boolean>>({});
  const save = useMutation({
    // How often stays as it is: an interval of 0 keeps it.
    mutationFn: () =>
      putEmailConfigsByIdNotifications(config.id, {
        notify: config.notify,
        digest_every: 0,
        mailboxes: changed,
      }),
    onSuccess: (saved) => {
      client.setQueryData<EmailConfig[]>(qk.emailConfigs, (old) =>
        old?.map((c) => (c.id === saved.id ? saved : c)),
      );
      client.setQueryData<Mailbox[]>(qk.mailboxes(config.id), (old) =>
        old?.map((mb) =>
          mb.id in changed ? { ...mb, notify: changed[mb.id]! } : mb,
        ),
      );
      onClose();
    },
  });

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Notify from"
      aside={<span className="truncate text-muted">{labelOf(config)}</span>}
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
            disabled={save.isPending || usable.length === 0}
          >
            {save.isPending ? "Saving" : "Save"}
          </Button>
        </>
      }
    >
      <form
        id={form}
        aria-label="Folders"
        className="flex flex-col gap-1.5"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        {!boxes ? (
          <p role="alert" className="text-sm text-accent">
            Unable to load folders. {messageOf(failed)}
          </p>
        ) : unknown(config, boxes) ? (
          config.sync_error ? (
            <p role="alert" className="text-sm text-accent">
              Unable to load folders. {config.sync_error}
            </p>
          ) : (
            <p className="text-sm text-muted">
              The folders have not been read from the server yet.
            </p>
          )
        ) : usable.length === 0 ? (
          <p className="text-sm text-muted">No folders.</p>
        ) : null}
        {usable.map((mb) => (
          <label
            key={mb.id}
            className="flex items-center gap-2 text-sm"
            style={{ paddingLeft: `${depthOf(mb) * 0.875}rem` }}
          >
            <input
              type="checkbox"
              checked={changed[mb.id] ?? mb.notify}
              onChange={(e) => {
                const on = e.target.checked;
                setChanged((c) => ({ ...c, [mb.id]: on }));
              }}
            />
            {labelOfMailbox(mb)}
          </label>
        ))}
      </form>
    </Dialog>
  );
}
