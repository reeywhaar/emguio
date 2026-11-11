import { useState } from "react";

import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { Field, Select } from "@app/components/Field";
import { clock, isoDay, weekday } from "@app/format";

const two = (n: number) => String(n).padStart(2, "0");

const day = (unix: number) => isoDay(new Date(unix * 1000));

/** Every day from one to another, as the date list offers them. */
function daysBetween(from: number, to: number): Date[] {
  const out: Date[] = [];
  const d = new Date(from * 1000);
  d.setHours(12, 0, 0, 0);
  while (isoDay(d) <= day(to) && out.length < 400) {
    out.push(new Date(d));
    d.setDate(d.getDate() + 1);
  }
  return out;
}

/** A morning some days from now, at 8:00. */
function morning(now: Date, ahead: number): number {
  const d = new Date(now);
  d.setDate(d.getDate() + ahead);
  d.setHours(8, 0, 0, 0);
  return Math.floor(d.getTime() / 1000);
}

/** Minutes go in fives, so a time picked is one the minute list has. */
const step = 5 * 60;

/** The usual times to send later, from now: those the server holds until. */
export function usual(now: Date, latest: number) {
  const at = Math.floor(now.getTime() / 1000);
  const monday = (8 - now.getDay()) % 7 || 7;
  const times = [
    { label: "In an hour", at: Math.ceil((at + 3600) / step) * step },
    {
      label: `Tomorrow, ${clock(new Date(morning(now, 1) * 1000))}`,
      at: morning(now, 1),
    },
  ];
  if (monday > 1) {
    times.push({
      label: `Monday, ${clock(new Date(morning(now, monday) * 1000))}`,
      at: morning(now, monday),
    });
  }
  return times.filter((t) => t.at <= latest);
}

/** This device's time zone, and its offset from UTC at a time: Asia/Dubai (GMT+4). */
function zone(unix: number): string {
  const name = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const offset = new Intl.DateTimeFormat("en", { timeZoneName: "shortOffset" })
    .formatToParts(new Date(unix * 1000))
    .find((p) => p.type === "timeZoneName")?.value;
  return offset ? `${name} (${offset})` : name;
}

/**
 * When a message goes, a while from now: one of the usual times, or a day and a time picked, up to
 * the latest the outgoing server holds mail until. See docs/sending.md.
 */
export function SendLater({
  open,
  latest,
  initial = null,
  changeable,
  onClose,
  onPick,
}: {
  open: boolean;
  latest: number;
  /** The time it was scheduled for, when it is opened again from Scheduled. */
  initial?: number | null;
  /** Whether the server shows what it holds in Scheduled, where it can be changed until it goes. */
  changeable: boolean;
  onClose: () => void;
  onPick: (at: number) => void;
}) {
  const [now] = useState(() => new Date());
  const times = usual(now, latest);
  const soonest = Math.floor(now.getTime() / 1000) + 120;
  const first =
    initial !== null && initial >= soonest && initial <= latest
      ? initial
      : (times[0]?.at ?? Math.floor(now.getTime() / 1000) + 3600);
  const [date, setDate] = useState(() => day(first));
  const [hour, setHour] = useState(() => new Date(first * 1000).getHours());
  const [minute, setMinute] = useState(() =>
    new Date(first * 1000).getMinutes(),
  );
  const [y, m, d] = date.split("-").map(Number);
  const at = Math.floor(
    new Date(y ?? NaN, (m ?? NaN) - 1, d, hour, minute).getTime() / 1000,
  );
  const fits = Number.isFinite(at) && at >= soonest && at <= latest;
  const choose = (t: number) => {
    const picked = new Date(t * 1000);
    setDate(day(t));
    setHour(picked.getHours());
    setMinute(picked.getMinutes());
  };
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
                aria-pressed={t.at === at}
                className="aria-pressed:border-brand"
                onClick={() => choose(t.at)}
              >
                {t.label}
              </Button>
            ))}
          </div>
        ) : null}
        <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
          <Field label="Date">
            <Select value={date} onChange={(e) => setDate(e.target.value)}>
              {daysBetween(soonest, latest).map((each) => (
                <option key={isoDay(each)} value={isoDay(each)}>
                  {isoDay(each)} {weekday(each)}
                </option>
              ))}
            </Select>
          </Field>
          <div role="group" aria-label="Time" className="flex flex-col gap-1">
            <span aria-hidden="true" className="caps text-sm text-muted">
              Time
            </span>
            <div className="flex items-center gap-1.5">
              <Select
                aria-label="Hour"
                className="w-20"
                value={hour}
                onChange={(e) => setHour(Number(e.target.value))}
              >
                {Array.from({ length: 24 }, (_, h) => (
                  <option key={h} value={h}>
                    {two(h)}
                  </option>
                ))}
              </Select>
              <span aria-hidden="true">:</span>
              <Select
                aria-label="Minute"
                className="w-20"
                value={minute}
                onChange={(e) => setMinute(Number(e.target.value))}
              >
                {Array.from({ length: 12 }, (_, i) => i * 5).map((mm) => (
                  <option key={mm} value={mm}>
                    {two(mm)}
                  </option>
                ))}
              </Select>
            </div>
          </div>
        </div>
        <p className="text-sm text-muted">
          Times are in this device's time zone,{" "}
          {zone(Number.isFinite(at) ? at : first)}. The mail server sends it at
          this time, even if this device is off.{" "}
          {changeable
            ? "Until then it waits in Scheduled, where you can change or cancel it."
            : "emguio cannot cancel it once it is scheduled."}
        </p>
      </div>
    </Dialog>
  );
}
