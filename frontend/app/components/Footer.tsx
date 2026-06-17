"use client";

export default function Footer() {
  return (
    <footer
      className="py-12 px-6"
      style={{ borderTop: "1px solid var(--border)" }}
    >
      <div className="max-w-6xl mx-auto flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <p
          className="text-xs font-mono"
          style={{ color: "var(--text-dim)" }}
        >
          Built on Soroban testnet · 29projects Lab · MIT License
        </p>
        <div className="flex items-center gap-6 text-xs font-mono">
          {[
            { label: "Docs", href: "https://docs.nectar.monster", external: true },
            { label: "Keeper SDK", href: "https://github.com/Nectar-Network/keeper-sdk", external: true },
            { label: "Media Kit", href: "/media-kit", external: false },
            { label: "Twitter", href: "https://x.com/nectar_xlm", external: true },
            { label: "GitHub", href: "https://github.com/Nectar-Network/nectar", external: true },
            { label: "Blend Protocol", href: "https://blend.capital", external: true },
          ].map((link) => (
            <a
              key={link.label}
              href={link.href}
              target={link.external ? "_blank" : undefined}
              rel={link.external ? "noopener noreferrer" : undefined}
              className="transition-colors duration-150"
              style={{ color: "var(--text-dim)" }}
              onMouseEnter={(e) => (e.currentTarget.style.color = "var(--text)")}
              onMouseLeave={(e) => (e.currentTarget.style.color = "var(--text-dim)")}
            >
              {link.label}
            </a>
          ))}
        </div>
      </div>
    </footer>
  );
}
