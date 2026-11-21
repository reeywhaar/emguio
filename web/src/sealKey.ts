/** The shortest seal key, as the server has it. */
export const SEAL_KEY_MIN = 12;

/** Why two typings of a new seal key will not do, or nothing. */
export function newKeyProblem(key: string, again: string): string {
  if (key.length < SEAL_KEY_MIN) {
    return `A seal key is at least ${SEAL_KEY_MIN} characters.`;
  }
  if (key !== again) return "The two seal keys do not match.";
  return "";
}
