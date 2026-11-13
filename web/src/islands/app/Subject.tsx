import { Tag } from "@app/components/Tag";

/** No subject at all, or the placeholder some clients write in for one, after any Re: or Fwd:. */
const none = /^((?:(?:re|fwd?)\s*:\s*)*)(?:\(no subject\))?$/i;

/** A message's subject, or a mark that it has none, rather than words standing in for one. */
export function Subject({ subject }: { subject: string }) {
  const empty = none.exec(subject.trim());
  if (!empty) return <>{subject}</>;
  return (
    <>
      {empty[1]}
      <Tag>No subject</Tag>
    </>
  );
}
