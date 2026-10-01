import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";

import { ConfirmProvider } from "@app/components/Confirm";

/**
 * A component under a query client of its own.
 *
 * Its own, per render: a client shared between tests carries one test's cached user into the
 * next one, and the failure looks like the component rather than the harness. Retries off, so a
 * test that asserts a refusal waits once rather than three times.
 */
export function mount(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return {
    ...render(
      <QueryClientProvider client={client}>
        <ConfirmProvider>{ui}</ConfirmProvider>
      </QueryClientProvider>,
    ),
    client,
  };
}
