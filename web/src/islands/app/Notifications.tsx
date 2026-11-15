import { useId, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getEmailConfigsByIdMailboxes,
  putEmailConfigsByIdNotifications,
} from "@app/api/actions/emailConfigs";
import {
  deleteDevicesById,
  getDevices,
  getPushSettings,
  postDevicesTest,
  putPushSettings,
} from "@app/api/actions/push";
import { qk } from "@app/api/keys";
import { ApiError, messageOf } from "@app/api/transport";
import type { EmailConfig, Mailbox, PushSettings, Quiet } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { Dummy } from "@app/components/Dummy";
import { Select } from "@app/components/Field";
import { WarnMark } from "@app/components/icons";
import { Switch } from "@app/components/Switch";
import { two } from "@app/format";
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
 * Which devices show the user's notifications, this one among them, and how they all come. How
 * each mail account's new mail is told is on its card. See docs/push.md.
 */
export function Notifications() {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="caps text-muted">Notifications</h2>
      <Devices />
      <Manner />
    </section>
  );
}

/** This device's time zone, which quiet hours turned on here are kept in. */
const zoneHere = () => Intl.DateTimeFormat().resolvedOptions().timeZone;

/** Quiet hours as first offered. */
const night = (): Quiet => ({
  from: "23:00",
  until: "07:00",
  zone: zoneHere(),
});

/** Whether they make a sound, and their quiet hours, in a few words; each part kept whole. */
function Summary({ settings, zone }: { settings: PushSettings; zone: string }) {
  const q = settings.quiet;
  return (
    <>
      <span className="whitespace-nowrap">
        {settings.silent ? "Silent" : "With sound"}
      </span>
      {" · "}
      <span className="whitespace-nowrap">
        {q ? `Quiet hours ${q.from}–${q.until}` : "No quiet hours"}
      </span>
      {q && q.zone !== zone ? (
        <>
          {" in "}
          <span className="whitespace-nowrap">{q.zone}</span>
        </>
      ) : null}
    </>
  );
}

/**
 * Whether notifications make a sound, and the hours none are sent, for every account and device:
 * in a line, and chosen in a dialog.
 */
function Manner() {
  const [opened, setOpened] = useState(0);
  const [open, setOpen] = useState(false);
  const settings = useQuery({
    queryKey: qk.pushSettings,
    queryFn: getPushSettings,
  });
  if (!settings.data) {
    return settings.error ? (
      <p role="alert" className="text-sm text-accent">
        {messageOf(settings.error)}
      </p>
    ) : (
      <Dummy className="h-11 w-full" />
    );
  }
  const zone = zoneHere();
  // Hours set on a device elsewhere are not this one's.
  const elsewhere =
    settings.data.quiet !== null && settings.data.quiet.zone !== zone;
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border border-line bg-bg px-4 py-2">
      {elsewhere ? (
        <p className="flex min-w-0 items-start gap-2 text-sm text-warn">
          <span className="mt-0.5 shrink-0">
            <WarnMark />
          </span>
          <span>
            <Summary settings={settings.data} zone={zone} />
          </span>
        </p>
      ) : (
        <p className="min-w-0 text-sm">
          <Summary settings={settings.data} zone={zone} />
        </p>
      )}
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
        <MannerDialog
          key={opened}
          settings={settings.data}
          open={open}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </div>
  );
}

/** Sound and quiet hours, changed together and saved at once. */
function MannerDialog({
  settings,
  open,
  onClose,
}: {
  settings: PushSettings;
  open: boolean;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const form = useId();
  const [draft, setDraft] = useState(settings);
  // Quiet hours turned off and on again come back as they were.
  const [last, setLast] = useState<Quiet>(() => settings.quiet ?? night());
  const save = useMutation({
    mutationFn: () => putPushSettings(draft),
    onSuccess: (saved) => {
      client.setQueryData(qk.pushSettings, saved);
      onClose();
    },
  });
  const quiet = draft.quiet;
  const zone = zoneHere();
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Sound and quiet hours"
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
        aria-label="Sound and quiet hours"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Choice
          title="With sound"
          note="iPhone and iPad ignore this and use the phone's own settings for emguio."
        >
          <Switch
            on={!draft.silent}
            label="With sound"
            onChange={(sound) => setDraft({ ...draft, silent: !sound })}
          />
        </Choice>
        <Choice
          title="Quiet hours"
          note="No notifications during these hours. When they end, each mail account sends one notification with its unread new mail."
        >
          <Switch
            on={quiet !== null}
            label="Quiet hours"
            onChange={(on) => {
              if (quiet) setLast(quiet);
              setDraft({ ...draft, quiet: on ? last : null });
            }}
          />
        </Choice>
        {quiet ? (
          <div className="flex flex-col gap-1.5">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              From
              <TimeSelect
                label="Start"
                value={quiet.from}
                onChange={(from) =>
                  setDraft({ ...draft, quiet: { ...quiet, from } })
                }
              />
              to
              <TimeSelect
                label="End"
                value={quiet.until}
                onChange={(until) =>
                  setDraft({ ...draft, quiet: { ...quiet, until } })
                }
              />
            </div>
            {quiet.zone === zone ? (
              <p className="text-xs text-muted">Times are in {zone}.</p>
            ) : (
              <p className="text-xs text-warn">
                Times are in {quiet.zone}, but this device is in {zone}.{" "}
                <button
                  type="button"
                  className="underline decoration-dashed underline-offset-2 hover:decoration-solid"
                  onClick={() =>
                    setDraft({ ...draft, quiet: { ...quiet, zone } })
                  }
                >
                  Use {zone}
                </button>
              </p>
            )}
          </div>
        ) : null}
      </form>
    </Dialog>
  );
}

/** One setting: what it is, a line on what it does, and its control at the right. */
function Choice({
  title,
  note,
  children,
}: {
  title: string;
  note: string;
  children: ReactNode;
}) {
  return (
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <p className="text-sm">{title}</p>
        <p className="text-xs text-muted">{note}</p>
      </div>
      {children}
    </div>
  );
}

/** Every half hour of a day, as HH:MM. */
const halfHours = Array.from(
  { length: 48 },
  (_, i) => `${two(Math.floor(i / 2))}:${i % 2 ? "30" : "00"}`,
);

/** A time of day as HH:MM, picked from the half hours. */
function TimeSelect({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  // A time set elsewhere stays choosable.
  const times = halfHours.includes(value)
    ? halfHours
    : [
        ...halfHours.filter((t) => t < value),
        value,
        ...halfHours.filter((t) => t > value),
      ];
  return (
    <Select
      size="bar"
      aria-label={label}
      className="w-24"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      {times.map((t) => (
        <option key={t} value={t}>
          {t}
        </option>
      ))}
    </Select>
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
