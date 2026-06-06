import { ReactNode } from "react";
import { TopBar } from "./TopBar";
import { MainNav } from "./MainNav";
import { SubNav } from "./SubNav";
import { Footer } from "./Footer";
import { BackToTop } from "./BackToTop";

type Props = {
  children: ReactNode;
};

export function AppShell({ children }: Props) {
  return (
    <div className="app-shell">
      <TopBar />
      <MainNav />
      <SubNav />
      <main className="app-shell__main">{children}</main>
      <Footer />
      <BackToTop />
    </div>
  );
}
