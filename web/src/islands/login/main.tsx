import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "@app/main.css";
import { Boundary } from "@app/components/Boundary";
import { Login } from "@app/islands/login/Login";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Boundary page>
      <Login />
    </Boundary>
  </StrictMode>,
);
