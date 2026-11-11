import { useState } from "react";

import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { Field } from "@app/components/Field";
import { TextField } from "@app/components/TextField";

const two = (n: number) => String(n).padStart(2, "0");

/** A time of day, as the reader's clock says it. */
const clock = (unix: number) =>
  new Date(unix * 1000).toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
  });

/** A time as a date and time field holds it: local, to the minute. */
function local(unix: number): string {
  const d = new Date(unix * 1000);
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}T${two(d.getHours())}:${two(d.getMinutes())}`;
}

/** A morning some days from now, at 8:00. */
function morning(now: Date, days: number): number {
  const d = new Date(now);
  d.setDate(d.getDate() + days);
  d.setHours(8, 0, 0, 0);
  return Math.floor(d.getTime() / 1000);
}

/** The usual times to send later, from now: those the server holds until. */
export function usual(now: Date, latest: number) {
  const at = Math.floor(now.getTime() / 1000);
  const monday = (8 - now.getDay()) % 7 || 7;
  const times = [
    { label: "In an hour", at: at + 3600 },
    { label: `Tomorrow, ${clock(morning(now, 1))}`, at: morning(now, 1) },
  ];
  if (monday > 1) {
    times.push({
      label: `Monday, ${clock(morning(now, monday))}`,
      at: morning(now, monday),
    });
  }
  return times.filter((t) => t.at <= latest);
}

/**
 * When a message goes, a while from now: one of the usual times, or one picked, up to the latest
 * the outgoing server holds mail until. See docs/sending.md.
 */
export function SendLater({
  open,
  latest,
  onClose,
  onPick,
}: {
  open: boolean;
  latest: number;
  onClose: () => void;
  onPick: (at: number) => void;
}) {
  const [now] = useState(() => new Date());
  const times = usual(now, latest);
  const [value, setValue] = useState(() =>
    local(times[0]?.at ?? Math.floor(now.getTime() / 1000) + 3600),
  );
  const at = Math.floor(new Date(value).getTime() / 1000);
  const soonest = Math.floor(now.getTime() / 1000) + 120;
  const fits = Number.isFinite(at) && at >= soonest && at <= latest;
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Send later"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="solid" disabled={!fits} onClick={() => onPick(at)}>
            Schedule
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {times.length > 0 ? (
          <div className="flex flex-wrap gap-2">
            {times.map((t) => (
              <Button
                key={t.label}
                size="bar"
                aria-pressed={local(t.at) === value}
                className="aria-pressed:border-brand"
                onClick={() => setValue(local(t.at))}
              >
                {t.label}
              </Button>
            ))}
          </div>
        ) : null}
        <Field label="Date and time">
          <TextField
            type="datetime-local"
            value={value}
            min={local(soonest)}
            max={local(latest)}
            onChange={(e) => setValue(e.target.value)}
          />
        </Field>
        <p className="text-sm text-muted">
          The mail server sends it at this time, even if this device is off.
          emguio cannot cancel it once it is scheduled.
        </p>
      </div>
    </Dialog>
  );
}
