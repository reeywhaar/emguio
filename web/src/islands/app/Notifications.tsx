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
import { Field, Select } from "@app/components/Field";
import { PencilMark } from "@app/components/icons";
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
  switch (state) {
    case "unsupported":
      sentence = "This browser cannot show notifications.";
      break;
    case "needs-install":
      sentence =
        "On iPhone and iPad, notifications come to emguio on the Home Screen. Tap Share, then Add to Home Screen, and turn them on from there.";
      break;
    case "denied":
      sentence =
        "Notifications are blocked for emguio here. Allow them in the browser's settings, then come back.";
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
        <p className="min-w-0 text-sm">{sentence}</p>
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

/** How one mail account's new mail is told, and the dialog that changes it. */
export function Delivery({ config }: { config: EmailConfig }) {
  const [opened, setOpened] = useState(0);
  const [open, setOpen] = useState(false);
  const boxes = useQuery({
    queryKey: qk.mailboxes(config.id),
    queryFn: () => getEmailConfigsByIdMailboxes(config.id),
  });
  const choice = choices.find((c) => c.value === choiceOf(config));
  return (
    <div className="flex items-center gap-1">
      <span className="min-w-0">
        {config.notify === "off" ? (
          "Never"
        ) : (
          <>
            {choice?.label}, from {boxes.data ? foldersOf(boxes.data) : "…"}
          </>
        )}
      </span>
      <Button
        variant="link"
        className="-my-1.5 shrink-0 rounded-md p-1.5 pointer-coarse:-my-2.5 pointer-coarse:p-2.5"
        aria-label="Change notifications"
        title="Change"
        disabled={!boxes.data}
        onClick={() => {
          setOpened((n) => n + 1);
          setOpen(true);
        }}
      >
        <PencilMark />
      </Button>
      {boxes.data && opened > 0 ? (
        <DeliveryDialog
          key={opened}
          config={config}
          boxes={boxes.data}
          open={open}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </div>
  );
}

function DeliveryDialog({
  config,
  boxes,
  open,
  onClose,
}: {
  config: EmailConfig;
  boxes: Mailbox[];
  open: boolean;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const form = useId();
  const [choice, setChoice] = useState(() => choiceOf(config));
  const [changed, setChanged] = useState<Record<string, boolean>>({});
  const save = useMutation({
    mutationFn: () => {
      const digest = choice !== "now" && choice !== "off";
      return putEmailConfigsByIdNotifications(config.id, {
        notify: digest ? "digest" : (choice as "now" | "off"),
        digest_every: digest ? Number(choice) : 0,
        mailboxes: changed,
      });
    },
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
      title="Notifications"
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
            disabled={save.isPending}
          >
            {save.isPending ? "Saving" : "Save"}
          </Button>
        </>
      }
    >
      <form
        id={form}
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field
          label="New mail"
          hint="A digest is one notification for all the mail that came in since the last one. Mail you have already read is left out."
        >
          <Select value={choice} onChange={(e) => setChoice(e.target.value)}>
            {choices.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </Select>
        </Field>
        <fieldset
          disabled={choice === "off"}
          className="flex flex-col gap-1.5 disabled:opacity-50"
        >
          <legend className="caps mb-1.5 text-sm text-muted">From</legend>
          {boxes
            .filter((mb) => mb.selectable)
            .map((mb) => (
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
        </fieldset>
      </form>
    </Dialog>
  );
}
