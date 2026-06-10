"use client";

import { usePathname } from "next/navigation";
import { useState } from "react";
import ThemeSwitch from "./ThemeSwitch";

const NAV_LINKS = [
  { label: "Features", href: "/features" },
  { label: "Vault", href: "/vault" },
  { label: "Performance", href: "/performance" },
  { label: "Dashboard", href: "/dashboard" },
];

// Active when the path equals the link, or is nested under it (e.g. /dashboard/keepers).
const isActive = (pathname: string, href: string) =>
  href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(href + "/");

const EXTERNAL_LINKS = [
  { label: "Twitter", href: "https://x.com/nectar_xlm" },
  { label: "GitHub", href: "https://github.com/nectar-network/nectar-poc" },
];

export default function Nav() {
  const pathname = usePathname();
  const [open, setOpen] = useState(false);

  return (
    <nav
      className="fixed top-0 left-0 right-0 z-50"
      style={{
        background: "var(--nav-bg, rgba(10, 11, 14, 0.85))",
        backdropFilter: "blur(12px)",
        WebkitBackdropFilter: "blur(12px)",
        borderBottom: "1px solid var(--border)",
      }}
    >
      <div className="flex items-center justify-between px-6 py-4">
        <div className="flex items-center gap-3">
          {/* Nectar mark — "the hive": six hairline keeper cells around one lit
              cell carrying the nectar drop. Compact variant for nav-scale (<=24px). */}
          <svg width="24" height="24" viewBox="0 0 48 48" fill="none" aria-label="Nectar">
            <polygon points="36.82,16.6 43.23,20.3 43.23,27.7 36.82,31.4 30.41,27.7 30.41,20.3" stroke="var(--text-mute)" strokeWidth="2" strokeLinejoin="round" fill="none" />
            <polygon points="11.18,16.6 17.59,20.3 17.59,27.7 11.18,31.4 4.77,27.7 4.77,20.3" stroke="var(--text-mute)" strokeWidth="2" strokeLinejoin="round" fill="none" />
            <polygon points="30.41,27.7 36.82,31.4 36.82,38.8 30.41,42.5 24,38.8 24,31.4" stroke="var(--text-mute)" strokeWidth="2" strokeLinejoin="round" fill="none" />
            <polygon points="30.41,5.5 36.82,9.2 36.82,16.6 30.41,20.3 24,16.6 24,9.2" stroke="var(--text-mute)" strokeWidth="2" strokeLinejoin="round" fill="none" />
            <polygon points="17.59,27.7 24,31.4 24,38.8 17.59,42.5 11.18,38.8 11.18,31.4" stroke="var(--text-mute)" strokeWidth="2" strokeLinejoin="round" fill="none" />
            <polygon points="17.59,5.5 24,9.2 24,16.6 17.59,20.3 11.18,16.6 11.18,9.2" stroke="var(--text-mute)" strokeWidth="2" strokeLinejoin="round" fill="none" />
            <polygon points="24,16.6 30.41,20.3 30.41,27.7 24,31.4 17.59,27.7 17.59,20.3" fill="var(--accent)" />
            <path d="M20.9 26.84 a3.1 3.1 0 1 0 6.2 0 C27.1 24.36 25.24 21.88 24 20.02 C22.76 21.88 20.9 24.36 20.9 26.84 Z" fill="var(--bg)" />
          </svg>
          <a
            href="/"
            className="font-syne font-700 tracking-widest text-sm"
            style={{ color: "var(--text)", letterSpacing: "0.2em", textDecoration: "none" }}
          >
            NECTAR
          </a>
        </div>

        {/* Desktop links */}
        <div className="hidden md:flex items-center gap-5">
          {NAV_LINKS.map((link) => {
            const active = isActive(pathname, link.href);
            return (
              <a
                key={link.href}
                href={link.href}
                className="text-xs font-mono transition-colors duration-200"
                style={{
                  color: active ? "var(--accent)" : "var(--text-dim)",
                  borderBottom: active ? "1px solid var(--accent)" : "1px solid transparent",
                  paddingBottom: "2px",
                }}
                onMouseEnter={(e) => {
                  if (!active) e.currentTarget.style.color = "var(--text)";
                }}
                onMouseLeave={(e) => {
                  if (!active) e.currentTarget.style.color = "var(--text-dim)";
                }}
              >
                {link.label}
              </a>
            );
          })}
          <ThemeSwitch />
          <span style={{ width: 1, height: 14, background: "var(--border)" }} />
          {EXTERNAL_LINKS.map((link) => (
            <a
              key={link.href}
              href={link.href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs font-mono transition-colors duration-200"
              style={{ color: "var(--text-dim)" }}
              onMouseEnter={(e) => (e.currentTarget.style.color = "var(--text)")}
              onMouseLeave={(e) => (e.currentTarget.style.color = "var(--text-dim)")}
            >
              {link.label}
            </a>
          ))}
        </div>

        {/* Mobile menu toggle */}
        <button
          type="button"
          className="md:hidden flex items-center justify-center"
          onClick={() => setOpen((v) => !v)}
          aria-label="Toggle navigation menu"
          aria-expanded={open}
          style={{ background: "transparent", border: "none", cursor: "pointer", padding: "4px", color: "var(--text)" }}
        >
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
            {open ? (
              <>
                <line x1="5" y1="5" x2="19" y2="19" />
                <line x1="19" y1="5" x2="5" y2="19" />
              </>
            ) : (
              <>
                <line x1="3" y1="7" x2="21" y2="7" />
                <line x1="3" y1="12" x2="21" y2="12" />
                <line x1="3" y1="17" x2="21" y2="17" />
              </>
            )}
          </svg>
        </button>
      </div>

      {/* Mobile dropdown */}
      {open && (
        <div
          className="md:hidden flex flex-col px-6 pb-4"
          style={{ borderTop: "1px solid var(--border)" }}
        >
          {NAV_LINKS.map((link) => {
            const active = isActive(pathname, link.href);
            return (
              <a
                key={link.href}
                href={link.href}
                onClick={() => setOpen(false)}
                className="py-3 text-sm font-mono"
                style={{
                  color: active ? "var(--accent)" : "var(--text-dim)",
                  borderBottom: "1px solid var(--border)",
                  textDecoration: "none",
                }}
              >
                {link.label}
              </a>
            );
          })}
          <div
            className="flex items-center justify-between py-3"
            style={{ borderBottom: "1px solid var(--border)" }}
          >
            <span
              className="text-xs font-mono"
              style={{ color: "var(--text-mute)", letterSpacing: "0.08em", textTransform: "uppercase" }}
            >
              Theme
            </span>
            <ThemeSwitch compact />
          </div>
          {EXTERNAL_LINKS.map((link) => (
            <a
              key={link.href}
              href={link.href}
              target="_blank"
              rel="noopener noreferrer"
              onClick={() => setOpen(false)}
              className="py-3 text-sm font-mono"
              style={{
                color: "var(--text-dim)",
                borderBottom: "1px solid var(--border)",
                textDecoration: "none",
              }}
            >
              {link.label}
            </a>
          ))}
        </div>
      )}
    </nav>
  );
}
