"use client";

import Architecture from "../components/Architecture";
import {
  Card,
  Btn,
  StatusDot,
  Eyebrow,
  SectionHead,
  keeperColor,
} from "../components/ds";

type CSS = React.CSSProperties;

const WRAP: CSS = { maxWidth: 1100, margin: "0 auto", padding: "0 24px" };

// ── Header ──────────────────────────────────────────────────────────────────
function Header() {
  return (
    <section style={{ paddingTop: 70, paddingBottom: 20 }}>
      <div style={WRAP}>
        <Eyebrow style={{ marginBottom: 16 }}>How it works</Eyebrow>
        <h1
          style={{
            fontFamily: "var(--font-display)",
            fontWeight: 800,
            fontSize: "clamp(2.4rem, 5vw, 3.6rem)",
            lineHeight: 1.05,
            letterSpacing: "-0.015em",
            color: "var(--text)",
            margin: "0 0 22px",
            maxWidth: 760,
          }}
        >
          A liquidation network with{" "}
          <span style={{ color: "var(--accent)" }}>no coordinator</span> and no
          single key.
        </h1>
        <p
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 14,
            lineHeight: 1.8,
            color: "var(--text-dim)",
            maxWidth: 560,
            margin: 0,
          }}
        >
          Stellar DeFi relies on single-operator bots for critical automation.
          Nectar distributes keeper responsibility across competing operators,
          funded by one shared vault.
        </p>
      </div>
    </section>
  );
}

// ── Flow — the keeper loop, deposit → monitor → race → settle → return ───────
const STEPS = [
  {
    n: "01",
    t: "Deposit",
    d: "LPs deposit USDC into the shared vault and receive appreciating LP shares.",
  },
  {
    n: "02",
    t: "Monitor",
    d: "Every keeper independently polls Blend pool positions for health factor < 1.0.",
  },
  {
    n: "03",
    t: "Race",
    d: "On an underwater position, all keepers create & submit fill transactions at once.",
  },
  {
    n: "04",
    t: "Settle",
    d: "First confirmed tx wins. Losers catch ErrAlreadyFilled and log it — no wasted gas spiral.",
  },
  {
    n: "05",
    t: "Return",
    d: "Winner returns 90% of profit to the vault; retains 10%. Share price ticks up.",
  },
];

function Flow() {
  return (
    <section
      style={{ padding: "84px 24px", borderTop: "1px solid var(--border)" }}
    >
      <div style={{ ...WRAP, padding: 0 }}>
        <SectionHead
          eyebrow="The loop"
          title="From deposit to liquidation, end to end"
        />
        <div className="features-flow">
          {STEPS.map((s, i) => (
            <div
              key={s.n}
              style={{
                padding: "4px 20px 4px 0",
                borderRight:
                  i < STEPS.length - 1 ? "1px solid var(--border)" : "none",
                paddingLeft: i ? 20 : 0,
              }}
            >
              <div
                style={{
                  fontFamily: "var(--font-display)",
                  fontWeight: 700,
                  fontSize: 26,
                  color: "var(--accent)",
                  marginBottom: 14,
                  fontVariantNumeric: "tabular-nums",
                }}
              >
                {s.n}
              </div>
              <div
                style={{
                  fontFamily: "var(--font-display)",
                  fontWeight: 700,
                  fontSize: 16,
                  color: "var(--text)",
                  marginBottom: 10,
                }}
              >
                {s.t}
              </div>
              <div
                style={{
                  fontFamily: "var(--font-mono)",
                  fontSize: 12,
                  lineHeight: 1.7,
                  color: "var(--text-dim)",
                }}
              >
                {s.d}
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

// ── Contention — ErrAlreadyFilled "graceful contention" replay ───────────────
// Illustrative replay (marketing copy, not a live feed): a representative
// position address and profit figure mirror the auction-contention pattern.
const CONTEND: { tag: string; msg: React.ReactNode; c: string }[] = [
  {
    tag: "keeper-alpha",
    msg: "pos GD7F9…2K1R hf=0.946 — LIQUIDATABLE",
    c: "var(--amber)",
  },
  {
    tag: "keeper-beta",
    msg: "pos GD7F9…2K1R hf=0.946 — LIQUIDATABLE",
    c: "var(--amber)",
  },
  {
    tag: "keeper-alpha",
    msg: "creating auction · submitting fill",
    c: "var(--text)",
  },
  {
    tag: "keeper-beta",
    msg: "creating auction · submitting fill",
    c: "var(--text)",
  },
  {
    tag: "keeper-alpha",
    msg: "filled auction: GD7F9…2K1R",
    c: "var(--accent)",
  },
  {
    tag: "keeper-beta",
    msg: "already filled by another keeper",
    c: "var(--text-mute)",
  },
  {
    tag: "keeper-alpha",
    msg: "returned drawn capital + profit to vault",
    c: "var(--info)",
  },
];

function Contention() {
  return (
    <section
      style={{ padding: "84px 24px", borderTop: "1px solid var(--border)" }}
    >
      <div className="features-two" style={{ ...WRAP, padding: 0 }}>
        <div>
          <Eyebrow style={{ marginBottom: 14 }}>Graceful contention</Eyebrow>
          <h2
            style={{
              fontFamily: "var(--font-display)",
              fontWeight: 700,
              fontSize: "clamp(1.4rem, 3vw, 2rem)",
              color: "var(--text)",
              margin: "0 0 18px",
              letterSpacing: "-0.01em",
            }}
          >
            Two keepers fill. One wins. Nothing breaks.
          </h2>
          <div style={{ borderLeft: "2px solid var(--border)", paddingLeft: 18 }}>
            <p
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 13,
                lineHeight: 1.8,
                color: "var(--text-dim)",
                margin: 0,
              }}
            >
              When a position goes underwater, multiple keepers submit fill
              transactions simultaneously. The first confirmed transaction wins
              the auction. The others receive{" "}
              <span style={{ color: "var(--text)" }}>ErrAlreadyFilled</span> and
              handle it gracefully — logging the miss, returning any drawn
              capital, and moving on. Redundancy without a coordinator, and
              without a single point of failure.
            </p>
          </div>
        </div>
        <Card style={{ padding: 0 }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              padding: "11px 16px",
              borderBottom: "1px solid var(--border)",
            }}
          >
            <StatusDot />
            <span
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 11,
                color: "var(--text-dim)",
              }}
            >
              auction contention — replay
            </span>
          </div>
          <div
            style={{
              padding: 16,
              display: "flex",
              flexDirection: "column",
              gap: 6,
            }}
          >
            {CONTEND.map((l, i) => (
              <div
                key={i}
                style={{
                  fontFamily: "var(--font-mono)",
                  fontSize: 11.5,
                  color: l.c,
                }}
              >
                <span style={{ color: "var(--text-mute)" }}>[</span>
                <span style={{ color: keeperColor(l.tag) }}>{l.tag}</span>
                <span style={{ color: "var(--text-mute)" }}>] </span>
                {l.msg}
              </div>
            ))}
          </div>
        </Card>
      </div>
    </section>
  );
}

// ── Economics — profit becomes share price ───────────────────────────────────
const ECON = [
  {
    t: "90 / 10 split",
    d: "Every liquidation returns 90% of realized profit to the vault. The winning keeper keeps 10% as its execution incentive.",
  },
  {
    t: "One rising price",
    d: "Profit accrues to a single share price — total_usdc ÷ total_shares. No reward tokens, no emissions, no lockups.",
  },
  {
    t: "Staked operators",
    d: "Keepers register on-chain and bond stake. Misbehavior is slashable; the leaderboard ranks by realized profit.",
  },
];

function Economics() {
  return (
    <section
      style={{ padding: "84px 24px", borderTop: "1px solid var(--border)" }}
    >
      <div style={{ ...WRAP, padding: 0 }}>
        <SectionHead
          eyebrow="Vault economics"
          title="Profit becomes share price"
          right={
            <Btn href="/dashboard" small>
              See it live →
            </Btn>
          }
        />
        <div className="features-econ">
          {ECON.map((e) => (
            <div
              key={e.t}
              style={{ borderLeft: "2px solid var(--accent)", paddingLeft: 18 }}
            >
              <div
                style={{
                  fontFamily: "var(--font-display)",
                  fontWeight: 700,
                  fontSize: 17,
                  color: "var(--text)",
                  marginBottom: 12,
                }}
              >
                {e.t}
              </div>
              <p
                style={{
                  fontFamily: "var(--font-mono)",
                  fontSize: 12.5,
                  lineHeight: 1.75,
                  color: "var(--text-dim)",
                  margin: 0,
                }}
              >
                {e.d}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

// ── Closing CTA ──────────────────────────────────────────────────────────────
function CTA() {
  return (
    <section
      style={{ padding: "84px 24px", borderTop: "1px solid var(--border)" }}
    >
      <div
        style={{
          ...WRAP,
          padding: 0,
          display: "flex",
          gap: 12,
          flexWrap: "wrap",
          alignItems: "center",
        }}
      >
        <h2
          style={{
            fontFamily: "var(--font-display)",
            fontWeight: 700,
            fontSize: "clamp(1.3rem, 2.4vw, 1.8rem)",
            color: "var(--text)",
            margin: "0 24px 0 0",
          }}
        >
          Ready to deposit?
        </h2>
        <Btn primary href="/vault">
          Open the vault
        </Btn>
        <Btn href="/dashboard">Live dashboard →</Btn>
      </div>
    </section>
  );
}

export default function FeaturesContent() {
  return (
    <div>
      <style>{`
        .features-flow { display: grid; grid-template-columns: repeat(5, 1fr); gap: 0; }
        .features-two { display: grid; grid-template-columns: 1fr 1fr; gap: 40px; align-items: center; }
        .features-econ { display: grid; grid-template-columns: repeat(3, 1fr); gap: 28px; }
        @media (max-width: 880px) {
          .features-flow { grid-template-columns: 1fr 1fr; }
          .features-two, .features-econ { grid-template-columns: 1fr; }
        }
      `}</style>
      <Header />
      <Flow />
      <Architecture />
      <Contention />
      <Economics />
      <CTA />
    </div>
  );
}
