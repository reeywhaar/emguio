import { useId, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  kindOf,
  postEmailConfigsByIdMailboxesByMailboxImport,
} from "@app/api/actions/imports";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Import, Mailbox } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { size } from "@app/format";
import { labelOfMailbox } from "@app/islands/app/mailbox";

/**
 * Importing mail from a file into a folder: the file chosen, then uploaded with how much has
 * gone. The import itself goes on in the background, its card in the corner. See
 * docs/reading.md.
 */
export function ImportDialog({
  config,
  folder,
  open,
  onClose,
}: {
  config: string;
  folder: Mailbox;
  open: boolean;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const form = useId();
  const picker = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [sent, setSent] = useState(0);
  const [problem, setProblem] = useState("");
  const name = labelOfMailbox(folder);

  const send = useMutation({
    mutationFn: ({ f, kind }: { f: File; kind: string }) =>
      postEmailConfigsByIdMailboxesByMailboxImport(
        config,
        folder.id,
        f,
        kind,
        (n, total) => setSent(total > 0 ? n / total : 0),
      ),
    onSuccess: (started) => {
      client.setQueryData<Import[]>(qk.imports, (old) => [
        ...(old ?? []),
        started,
      ]);
      void client.invalidateQueries({ queryKey: qk.imports });
      onClose();
    },
  });

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={`Import mail into ${name}`}
      footer={
        <>
          {problem || send.error ? (
            <p role="alert" className="w-full text-sm text-accent">
              {problem || messageOf(send.error)}
            </p>
          ) : null}
          <Button onClick={onClose}>Cancel</Button>
          <Button
            type="submit"
            form={form}
            variant="solid"
            disabled={send.isPending}
          >
            {send.isPending ? "Uploading" : "Import"}
          </Button>
        </>
      }
    >
      <form
        id={form}
        aria-label={`Import mail into ${name}`}
        className="flex flex-col gap-3 text-sm"
        onSubmit={(e) => {
          e.preventDefault();
          const kind = file && kindOf(file.name);
          const why = !file
            ? "Choose a file to import."
            : !kind
              ? "Import an .mbox, an .eml, or a .zip of them."
              : "";
          setProblem(why);
          if (file && kind) {
            setSent(0);
            send.mutate({ f: file, kind });
          }
        }}
      >
        <p className="text-muted">
          An .mbox or an .eml goes into {name}. A .zip of them is made a tree of
          folders inside it: zip a folder of .eml files to import it. Mail that
          is there already is passed over.
        </p>
        <input
          ref={picker}
          type="file"
          accept=".mbox,.eml,.zip"
          aria-label="File to import"
          className="hidden"
          onChange={(e) => {
            setFile(e.target.files?.[0] ?? null);
            setProblem("");
          }}
        />
        <div className="flex items-center gap-3">
          <Button
            size="bar"
            className="shrink-0"
            disabled={send.isPending}
            onClick={() => picker.current?.click()}
          >
            Choose a file…
          </Button>
          <span className="min-w-0 truncate text-muted">
            {file ? `${file.name}, ${size(file.size)}` : "None chosen"}
          </span>
        </div>
        {send.isPending ? (
          <div className="flex flex-col gap-1">
            <div
              aria-hidden="true"
              className="h-1 overflow-hidden rounded-full bg-fill"
            >
              <div
                className="h-full rounded-full bg-brand transition-[width]"
                style={{ width: `${100 * sent}%` }}
              />
            </div>
            <span className="text-xs text-muted tabular-nums">
              Uploading, {Math.round(100 * sent)}%
            </span>
          </div>
        ) : null}
      </form>
    </Dialog>
  );
}
