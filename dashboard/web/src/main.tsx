import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { SnapshotProvider } from "./SnapshotContext.js";
import { App } from "./App.js";
import "./styles/global.css";

const root = document.getElementById("root");
if (!root) throw new Error("Root element not found");

createRoot(root).render(
  <StrictMode>
    <BrowserRouter>
      <SnapshotProvider>
        <App />
      </SnapshotProvider>
    </BrowserRouter>
  </StrictMode>
);
