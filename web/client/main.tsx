import React from "react";
import { createRoot } from "react-dom/client";
import { Meteor } from "meteor/meteor";
import { App } from "../imports/ui/App";
Meteor.startup(() =>
  createRoot(document.getElementById("root")!).render(<App />),
);
