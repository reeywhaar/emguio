import { useState } from "react";

import { TextField } from "@app/components/TextField";
import { SEAL_KEY_MIN } from "@app/sealKey";

/** Without letters that pass for one another: no i, l, o or u. */
const ALPHABET = "0123456789abcdefghjkmnpqrstvwxyz";

/** A seal key nobody has to think of: twenty characters of that alphabet, 100 bits, in fours. */
export function generateSealKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(20));
  const chars = [...bytes].map((b) => ALPHABET[b % ALPHABET.length]);
  return [0, 4, 8, 12, 16]
    .map((at) => chars.slice(at, at + 4).join(""))
    .join("-");
}

/**
 * A new seal key, typed twice, or generated: shown then, to copy and keep, since it is asked for
 * after every restart and nothing recovers it.
 */
export function NewSealKey({
  label,
  name,
  value,
  again,
  onChange,
  autoFocus = false,
}: {
  /** What the first field is called; the second is it "again". */
  label: string;
  /** The first field's name; the second's ends in "-again". */
  name: string;
  value: string;
  again: string;
  onChange: (value: string, again: string) => void;
  autoFocus?: boolean;
}) {
  const [shown, setShown] = useState(false);
  const [copied, setCopied] = useState(false);
  const type = shown ? "text" : "password";
  return (
    <div className="flex flex-col gap-3">
      <TextField
        name={name}
        type={type}
        autoComplete="new-password"
        aria-label={label}
        placeholder={`${label}, ${SEAL_KEY_MIN} characters or more`}
        className="w-full"
        value={value}
        onChange={(e) => onChange(e.target.value, again)}
        autoFocus={autoFocus}
      />
      <TextField
        name={`${name}-again`}
        type={type}
        autoComplete="new-password"
        aria-label={`${label} again`}
        placeholder={`${label} again`}
        className="w-full"
        value={again}
        onChange={(e) => onChange(value, e.target.value)}
      />
      <div className="flex items-center gap-4 text-sm">
        <button
          type="button"
          className="text-muted underline underline-offset-2 hover:text-fg"
          onClick={() => {
            const key = generateSealKey();
            onChange(key, key);
            setShown(true);
            setCopied(false);
          }}
        >
          Generate one
        </button>
        {shown && value !== "" ? (
          <button
            type="button"
            className="text-muted underline underline-offset-2 hover:text-fg"
            onClick={() =>
              void navigator.clipboard
                ?.writeText(value)
                .then(() => setCopied(true))
            }
          >
            {copied ? "Copied" : "Copy"}
          </button>
        ) : null}
      </div>
      {shown ? (
        <p className="text-xs text-muted">
          Save it somewhere safe before you go on: emguio asks for it after
          every restart, and nothing can recover it.
        </p>
      ) : null}
    </div>
  );
}
