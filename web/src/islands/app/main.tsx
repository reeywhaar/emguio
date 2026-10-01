import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";

import { sessionClient } from "@app/api/client";
import "@app/main.css";
import { Boundary } from "@app/components/Boundary";
import { App } from "@app/islands/app/App";

const client = sessionClient();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Boundary page>
      <QueryClientProvider client={client}>
        <App />
      </QueryClientProvider>
    </Boundary>
  </StrictMode>,
);
