import {
  MutationCache,
  notifyManager,
  QueryCache,
  QueryClient,
} from "@tanstack/react-query";

import { ApiError } from "@app/api/transport";
import { leaveFor } from "@app/leave";

/**
 * Every read is a server read. Query owns what came from the server; useState owns the rest,
 * except what is in the URL, which the URL owns.
 */

// One navigation however many requests failed together: everything on screen answers 401 at the
// same moment when a session ends.
let leaving = false;

/**
 * Sends the tab to the sign-in page when the session it was using has ended.
 *
 * Not in transport, which cannot tell the two 401s apart: the sign-in form gets one for a
 * wrong password and has to stay on the page to say so. An island that has already established
 * it needs a session is the thing that can read the same status as "it ended".
 */
function ended(error: unknown) {
  if (leaving || !(error instanceof ApiError) || error.status !== 401) return;
  leaving = true;
  leaveFor("/login");
}

// What a press asks is drawn before the next paint: by default a query's news reaches the
// screen on a timer, a frame after the press, and a message opened unread shows unread for that
// frame before it is drawn read.
notifyManager.setScheduler(queueMicrotask);

/** The client for an island that requires a session. */
export function sessionClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
    queryCache: new QueryCache({ onError: ended }),
    mutationCache: new MutationCache({ onError: ended }),
  });
}
