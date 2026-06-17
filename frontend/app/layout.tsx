import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Nectar Network — Keeper Infrastructure for Soroban DeFi",
  description:
    "Multi-operator liquidation and keeper infrastructure for Blend Protocol on Stellar. SCF #42 submission by 29projects Lab.",
  openGraph: {
    title: "Nectar Network",
    description: "Multi-operator keeper infrastructure for Soroban DeFi",
    type: "website",
  },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="antialiased">
        {/* Apply the persisted theme before paint to avoid a flash. */}
        <script
          dangerouslySetInnerHTML={{
            __html:
              "try{document.documentElement.dataset.vibe=localStorage.getItem('nectar.vibe')||'terminal'}catch(e){document.documentElement.dataset.vibe='terminal'}",
          }}
        />
        {children}
      </body>
    </html>
  );
}
