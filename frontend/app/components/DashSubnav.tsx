"use client";

import { usePathname } from "next/navigation";
import { StatusDot } from "./ds";

const TABS = [
  { id: "overview", label: "Overview", href: "/dashboard" },
  { id: "keepers", label: "Keepers", href: "/dashboard/keepers" },
  { id: "liquidations", label: "Liquidations", href: "/dashboard/liquidations" },
  { id: "depositor", label: "Depositor", href: "/dashboard/depositor" },
];

export default function DashSubnav() {
  const pathname = usePathname();
  const activeId = (() => {
    if (pathname.startsWith("/dashboard/keepers")) return "keepers";
    if (pathname.startsWith("/dashboard/liquidations")) return "liquidations";
    // /dashboard/depositor or a direct /dashboard/<address> position link
    if (pathname.startsWith("/dashboard/depositor") || /^\/dashboard\/[^/]+$/.test(pathname)) return "depositor";
    return "overview";
  })();

  return (
    <div
      style={{
        position: "sticky", top: 57, zIndex: 40,
        background: "var(--nav-bg, rgba(10,11,14,0.85))",
        backdropFilter: "blur(12px)", WebkitBackdropFilter: "blur(12px)",
        borderBottom: "1px solid var(--border)",
      }}
    >
      <div style={{ maxWidth: 1100, margin: "0 auto", padding: "0 16px", display: "flex", alignItems: "center", gap: 4, overflowX: "auto" }} className="thin-scroll">
        {TABS.map((tab) => {
          const a = activeId === tab.id;
          return (
            <a
              key={tab.id}
              href={tab.href}
              style={{
                fontFamily: "var(--font-mono)", fontSize: 12, textDecoration: "none",
                padding: "13px 14px", color: a ? "var(--text)" : "var(--text-dim)",
                borderBottom: `2px solid ${a ? "var(--accent)" : "transparent"}`,
                transition: "color 200ms", display: "flex", alignItems: "center", gap: 7,
                flexShrink: 0, whiteSpace: "nowrap",
              }}
            >
              {a && <StatusDot size={5} />}
              {tab.label}
            </a>
          );
        })}
        <a
          href="/performance"
          className="hide-mobile"
          style={{ marginLeft: "auto", fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--text-mute)", textDecoration: "none", whiteSpace: "nowrap", flexShrink: 0 }}
        >
          legacy performance view →
        </a>
      </div>
    </div>
  );
}
