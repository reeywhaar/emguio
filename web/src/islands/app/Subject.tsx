import { Tag } from "@app/components/Tag";

/**
 * No subject at all, or the placeholder a client writes in for one — "(No subject)", "No
 * Subject", "Без темы" — after any Re: or Fwd:, however many.
 */
const none =
  /^(?:(?:re|fwd?)(?:\[\d+\])?\s*:\s*)*(?:[([]?\s*(?:no subject|без темы)\s*[)\]]?)?$/i;

/** Whether a message has no subject, or only a placeholder for one. */
export const untitled = (subject: string) => none.test(subject.trim());

/** A message's subject, or a mark that it has none, rather than words standing in for one. */
export function Subject({ subject }: { subject: string }) {
  return untitled(subject) ? <Tag>No subject</Tag> : <>{subject}</>;
}
