import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { qk } from "@app/api/keys";

/**
 * Keeps an open tab in step with the mail arriving underneath it.
 *
 * The server says only that something changed, so this refetches what is on screen rather than
 * patching a cache from a payload — one round trip against a second copy of the model.
 *
 * EventSource rather than a socket: it reconnects on its own with backoff.
 */
export function useLive() {
  const client = useQueryClient();

  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    const source = new EventSource("/api/events");
    const refresh = () => {
      client.invalidateQueries({ queryKey: qk.mail });
      client.invalidateQueries({ queryKey: qk.emailConfigs });
    };
    source.addEventListener("changed", refresh);
    // A reconnection means the stream was down, and whatever happened meanwhile was not said.
    // Asking once on the way back is cheaper than tracking what was missed.
    source.addEventListener("open", refresh);
    return () => source.close();
  }, [client]);
}
