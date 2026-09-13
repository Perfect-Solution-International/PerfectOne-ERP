import "./globals.css";
import "./liquid-glass.css";
import { Suspense } from "react";
import PWARegister from "./components/PWARegister";
import OfflineBanner from "./components/OfflineBanner";

export const metadata = { title: { default: "PerfectOne ERP", template: "%s | PerfectOne ERP" }, description: "One intelligent platform for complete retail business control", applicationName: "PerfectOne ERP" };

// PerfectOne ERP is dark-only by design — force it before paint so nothing ever flashes light.
const themeBootstrap = `(function(){try{document.documentElement.dataset.theme="dark";document.documentElement.style.colorScheme="dark";localStorage.removeItem("grocerly_theme");}catch(e){}})();`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-theme="dark" style={{ colorScheme: "dark" }}>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeBootstrap }} />
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
        <link
          href="https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:ital,wght@0,400;0,500;0,600;0,700;0,800;1,500&family=Spline+Sans+Mono:wght@400;500;600&display=swap"
          rel="stylesheet"
        />
      </head>
      <body>
        <PWARegister />
        <OfflineBanner />
        <Suspense fallback={<div className="page">Loading…</div>}>{children}</Suspense>
      </body>
    </html>
  );
}
