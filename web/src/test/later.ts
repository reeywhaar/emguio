/** An answer given when the test says. */
export function later<T>() {
  const out = {} as {
    promise: Promise<T>;
    give: (value: T) => void;
    refuse: (err: Error) => void;
  };
  out.promise = new Promise<T>((give, refuse) => {
    out.give = give;
    out.refuse = refuse;
  });
  return out;
}
