import type { ReactNode } from "react";
import { SideNav } from "./AppNavigation";
import { TopNav } from "./TopNav";

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="app-shell" data-ui="app-shell">
      <SideNav />
      <TopNav />
      <div className="app-body">{children}</div>
    </div>
  );
}
