// Nectar — Media Kit. Recreated from the design-system handoff
// (project/media-kit/index.html) as a Next route. Brand assets live in
// /public/media-kit (svg / png / jpg). Styles are scoped under `.mk` so they
// never leak into the rest of the app; tokens not in globals.css are defined
// locally on the wrapper.

type Asset = {
  name: string;
  files: string;
  svg?: string;
  png?: string;
  jpg?: string;
};

const ASSETS: Asset[] = [
  {
    name: "Primary mark",
    files: "SVG · PNG · JPG — 1024px",
    svg: "/media-kit/svg/nectar-mark.svg",
    png: "/media-kit/logo-files/nectar-mark.png",
    jpg: "/media-kit/logo-files/nectar-mark.jpg",
  },
  {
    name: "Compact mark",
    files: "SVG · PNG · JPG — solid cell, ≤24px",
    svg: "/media-kit/svg/nectar-mark-compact.svg",
    png: "/media-kit/logo-files/nectar-mark-compact.png",
    jpg: "/media-kit/logo-files/nectar-mark-compact.jpg",
  },
  {
    name: "Monochrome mark — white",
    files: "SVG (currentColor) · PNG · JPG",
    svg: "/media-kit/svg/nectar-mark-mono.svg",
    png: "/media-kit/logo-files/nectar-mark-mono-white.png",
    jpg: "/media-kit/logo-files/nectar-mark-mono-white.jpg",
  },
  {
    name: "Monochrome mark — black",
    files: "PNG · JPG (light backgrounds)",
    png: "/media-kit/logo-files/nectar-mark-mono-black.png",
    jpg: "/media-kit/logo-files/nectar-mark-mono-black.jpg",
  },
  {
    name: "Lockup",
    files: "SVG · PNG · JPG — mark + wordmark",
    svg: "/media-kit/svg/nectar-lockup.svg",
    png: "/media-kit/logo-files/nectar-lockup.png",
    jpg: "/media-kit/logo-files/nectar-lockup.jpg",
  },
  {
    name: "Lockup — mono",
    files: "SVG · PNG · JPG",
    svg: "/media-kit/svg/nectar-lockup-mono.svg",
    png: "/media-kit/logo-files/nectar-lockup-mono.png",
    jpg: "/media-kit/logo-files/nectar-lockup-mono.jpg",
  },
  {
    name: "App icon",
    files: "SVG · PNG · JPG — 1024 / 512 / 256 / 192 / 180",
    svg: "/media-kit/svg/nectar-app-icon.svg",
    png: "/media-kit/logo-files/nectar-app-icon.png",
    jpg: "/media-kit/logo-files/nectar-app-icon.jpg",
  },
  {
    name: "Favicon",
    files: "PNG 48 · 32 · 16",
    png: "/media-kit/png/favicon/nectar-favicon-32.png",
  },
  {
    name: "Social avatar",
    files: "PNG 1024 (square, circle-safe)",
    png: "/media-kit/png/avatar/nectar-avatar-1024.png",
  },
];

const STYLES = `
.mk {
  --r-soft: 4px;
  --r-sharp: 2px;
  --text-mute: hsl(220, 10%, 35%);
  --font-display: "Syne", "Helvetica Neue", sans-serif;
  --font-mono: "DM Mono", ui-monospace, "SFMono-Regular", monospace;
  color: var(--text);
}
.mk .wrap { max-width: 1080px; margin: 0 auto; padding: 40px 32px 96px; }
.mk .eyebrow { font-family: var(--font-mono); font-size: 11px; letter-spacing: 0.14em; text-transform: uppercase; color: var(--text-dim); }
.mk h1 { font-family: var(--font-display); font-weight: 800; font-size: clamp(34px, 6vw, 56px); letter-spacing: -0.015em; margin: 14px 0 0; line-height: 1.02; color: var(--text); }
.mk .lede { font-family: var(--font-mono); font-size: 14px; line-height: 1.75; color: var(--text-dim); max-width: 640px; margin: 22px 0 0; }
.mk .lede b { color: var(--text); font-weight: 400; }
.mk section { border-top: 1px solid var(--border); margin-top: 64px; padding-top: 40px; }
.mk .sec-head { display: flex; align-items: baseline; gap: 14px; margin-bottom: 28px; }
.mk .sec-head .n { font-family: var(--font-mono); font-size: 11px; color: var(--accent); letter-spacing: 0.1em; }
.mk .sec-head h2 { font-family: var(--font-display); font-weight: 700; font-size: 22px; margin: 0; color: var(--text); }
.mk .note { font-family: var(--font-mono); font-size: 12px; color: var(--text-dim); line-height: 1.7; max-width: 560px; }
.mk .note b { color: var(--text); font-weight: 400; }
.mk .mt-14 { margin-top: 14px; }
.mk .mt-18 { margin-top: 18px; }

.mk .hero-mark { display: grid; grid-template-columns: 280px 1fr; gap: 40px; align-items: center; }
.mk .plate { background: var(--bg); border: 1px solid var(--border); border-radius: var(--r-soft); display: flex; align-items: center; justify-content: center; }
.mk .hero-mark .plate { height: 280px; }
.mk .hero-mark .plate img { width: 184px; height: 184px; }

.mk .two { display: grid; grid-template-columns: 1fr 1fr; gap: 20px; }
.mk .lockcard { border: 1px solid var(--border); border-radius: var(--r-soft); overflow: hidden; }
.mk .lockcard .img { height: 150px; display: flex; align-items: center; justify-content: center; background: var(--bg); }
.mk .lockcard .img.darker { background: #0e1014; }
.mk .lockcard .img img { height: 52px; }
.mk .lockcard .cap { font-family: var(--font-mono); font-size: 11px; color: var(--text-dim); padding: 12px 16px; border-top: 1px solid var(--border); }

.mk .clear { display: flex; gap: 40px; align-items: center; flex-wrap: wrap; }
.mk .csbox { position: relative; width: 260px; height: 260px; border: 1px solid var(--border); border-radius: var(--r-soft); display: flex; align-items: center; justify-content: center; }
.mk .csbox .pad { position: absolute; inset: 56px; border: 1px dashed hsla(160, 90%, 52%, 0.5); border-radius: 3px; }
.mk .csbox img { width: 148px; height: 148px; position: relative; }

.mk .sizes { display: flex; align-items: flex-end; gap: 36px; }
.mk .sizeitem { display: flex; flex-direction: column; align-items: center; gap: 12px; }
.mk .sizeitem .label { font-family: var(--font-mono); font-size: 10px; color: var(--text-mute); }
.mk .sizeitem .plate { padding: 18px; border-radius: var(--r-sharp); }

.mk .swatches { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 14px; }
.mk .sw { border: 1px solid var(--border); border-radius: var(--r-soft); overflow: hidden; }
.mk .sw .chip { height: 76px; }
.mk .sw .meta { padding: 10px 14px; font-family: var(--font-mono); font-size: 11px; color: var(--text-dim); line-height: 1.6; }
.mk .sw .meta b { color: var(--text); font-weight: 400; display: block; }

.mk .icons { display: flex; align-items: flex-end; gap: 28px; flex-wrap: wrap; }
.mk .icons .ic { display: flex; flex-direction: column; align-items: center; gap: 10px; }
.mk .icons .ic img { border-radius: 22%; display: block; }
.mk .icons .ic span { font-family: var(--font-mono); font-size: 10px; color: var(--text-mute); }

.mk .donts { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 14px; }
.mk .dont { border: 1px solid var(--border); border-radius: var(--r-soft); overflow: hidden; }
.mk .dont .box { height: 120px; display: flex; align-items: center; justify-content: center; background: var(--bg); }
.mk .dont .box img { width: 72px; height: 72px; }
.mk .dont .x { font-family: var(--font-mono); font-size: 11px; color: var(--red); padding: 9px 12px; border-top: 1px solid var(--border); display: flex; gap: 7px; align-items: center; }
.mk .dont .x::before { content: "\\2715"; }

.mk .dl { display: flex; gap: 10px; flex-wrap: wrap; margin-top: 22px; }
.mk .dl a { font-family: var(--font-mono); font-size: 11px; letter-spacing: 0.05em; text-transform: uppercase; color: var(--text-dim); text-decoration: none; border: 1px solid var(--border); border-radius: var(--r-sharp); padding: 8px 16px; transition: color 200ms var(--ease-out, ease-out), border-color 200ms var(--ease-out, ease-out); }
.mk .dl a:hover { color: var(--accent); border-color: var(--accent); }

.mk table { width: 100%; border-collapse: collapse; font-family: var(--font-mono); font-size: 12px; }
.mk th { text-align: left; font-weight: 400; color: var(--text-dim); font-size: 10px; letter-spacing: 0.1em; text-transform: uppercase; padding: 0 0 12px; border-bottom: 1px solid var(--border); }
.mk td { padding: 13px 0; border-bottom: 1px solid var(--border); color: var(--text); vertical-align: middle; }
.mk td.files { color: var(--text-dim); }
.mk td a { color: var(--accent); text-decoration: none; margin-right: 14px; }
.mk td a:hover { text-decoration: underline; }

@media (max-width: 720px) {
  .mk .hero-mark { grid-template-columns: 1fr; }
  .mk .two { grid-template-columns: 1fr; }
}
`;

export default function MediaKitContent() {
  return (
    <div className="mk">
      <style dangerouslySetInnerHTML={{ __html: STYLES }} />
      <div className="wrap">
        <header>
          <div className="eyebrow">Brand · Media Kit</div>
          <h1>Nectar — The Hive</h1>
          <p className="lede">
            The mark is a <b>honeycomb of seven cells</b> — the distributed keeper
            network — with one cell lit and carrying the <b>nectar droplet</b>: the
            shared vault every keeper feeds. One brand system: electric mint on
            near-black, Syne + DM Mono. Below: usage rules and every asset in SVG,
            PNG &amp; JPG.
          </p>
        </header>

        {/* 01 The mark */}
        <section>
          <div className="sec-head">
            <span className="n">01</span>
            <h2>The mark</h2>
          </div>
          <div className="hero-mark">
            <div className="plate">
              <img src="/media-kit/svg/nectar-mark.svg" alt="Nectar mark" />
            </div>
            <div className="note">
              <b>Primary mark.</b> Six hairline cells frame a single accent cell —
              the live keeper — holding the nectar drop. The lit cell uses the
              brand&apos;s &ldquo;active position&rdquo; treatment: accent border +
              faint accent fill. Never recolor the drop; it is always the accent.
              <br />
              <br />
              Use the primary mark at <b>32px and above</b>. Below that, switch to
              the compact mark (solid center cell) so it stays legible.
            </div>
          </div>
          <div className="dl">
            <a href="/media-kit/svg/nectar-mark.svg" download>
              Download SVG
            </a>
            <a href="/media-kit/logo-files/nectar-mark.png" download>
              Download PNG
            </a>
            <a href="/media-kit/logo-files/nectar-mark.jpg" download>
              Download JPG
            </a>
          </div>
        </section>

        {/* 02 Lockup */}
        <section>
          <div className="sec-head">
            <span className="n">02</span>
            <h2>Lockup</h2>
          </div>
          <div className="two">
            <div className="lockcard">
              <div className="img">
                <img
                  src="/media-kit/png/lockup/nectar-lockup-on-dark.png"
                  alt="Nectar lockup"
                />
              </div>
              <div className="cap">Primary — mint mark + NECTAR / NETWORK</div>
            </div>
            <div className="lockcard">
              <div className="img darker">
                <img
                  src="/media-kit/png/lockup/nectar-lockup-mono-on-dark.png"
                  alt="Nectar lockup mono"
                />
              </div>
              <div className="cap">
                Monochrome — single-color reversal for dense / one-ink use
              </div>
            </div>
          </div>
          <p className="note mt-18">
            Wordmark is <b>Syne 700</b>, tracked <b>0.2em</b>, with a{" "}
            <b>DM Mono</b> NETWORK sub-label. Keep the mark-to-wordmark gap and
            never substitute the typeface.
          </p>
          <div className="dl">
            <a href="/media-kit/svg/nectar-lockup.svg" download>
              Download SVG
            </a>
            <a href="/media-kit/logo-files/nectar-lockup.png" download>
              Download PNG
            </a>
            <a href="/media-kit/logo-files/nectar-lockup.jpg" download>
              Download JPG
            </a>
          </div>
        </section>

        {/* 03 Clear space + min size */}
        <section>
          <div className="sec-head">
            <span className="n">03</span>
            <h2>Clear space &amp; minimum size</h2>
          </div>
          <div className="clear">
            <div>
              <div className="csbox">
                <span className="pad" />
                <img src="/media-kit/svg/nectar-mark.svg" alt="clear space" />
              </div>
              <p className="note mt-14">
                Keep clear space of <b>one cell</b> on every side. Nothing crosses
                the dashed boundary.
              </p>
            </div>
            <div>
              <div className="sizes">
                <div className="sizeitem">
                  <div className="plate">
                    <img
                      src="/media-kit/svg/nectar-mark.svg"
                      width={48}
                      height={48}
                      alt=""
                    />
                  </div>
                  <div className="label">48px · primary</div>
                </div>
                <div className="sizeitem">
                  <div className="plate">
                    <img
                      src="/media-kit/svg/nectar-mark.svg"
                      width={32}
                      height={32}
                      alt=""
                    />
                  </div>
                  <div className="label">32px · primary min</div>
                </div>
                <div className="sizeitem">
                  <div className="plate">
                    <img
                      src="/media-kit/png/favicon/nectar-favicon-16.png"
                      width={16}
                      height={16}
                      alt=""
                    />
                  </div>
                  <div className="label">16px · compact</div>
                </div>
              </div>
              <p className="note mt-14">
                <b>Primary mark:</b> never below 32px. <b>Compact mark</b> (solid
                cell) covers 16–24px favicons.
              </p>
            </div>
          </div>
        </section>

        {/* 04 Color */}
        <section>
          <div className="sec-head">
            <span className="n">04</span>
            <h2>Color</h2>
          </div>
          <div className="swatches">
            <div className="sw">
              <div className="chip" style={{ background: "hsl(160, 90%, 52%)" }} />
              <div className="meta">
                <b>Accent mint</b>hsl(160 90% 52%)
              </div>
            </div>
            <div className="sw">
              <div className="chip" style={{ background: "hsl(220, 15%, 6%)" }} />
              <div className="meta">
                <b>Canvas</b>hsl(220 15% 6%)
              </div>
            </div>
            <div className="sw">
              <div className="chip" style={{ background: "hsl(220, 15%, 24%)" }} />
              <div className="meta">
                <b>Cell hairline</b>hsl(220 15% 24%)
              </div>
            </div>
            <div className="sw">
              <div className="chip" style={{ background: "hsl(220, 20%, 75%)" }} />
              <div className="meta">
                <b>Wordmark text</b>hsl(220 20% 75%)
              </div>
            </div>
          </div>
          <p className="note mt-18">
            The brand is <b>dark-only</b>. On light or photographic backgrounds,
            use the <b>monochrome</b> mark (white knockout or single ink). The mint
            accent never sits on white.
          </p>
        </section>

        {/* 05 App icon */}
        <section>
          <div className="sec-head">
            <span className="n">05</span>
            <h2>App icon</h2>
          </div>
          <div className="icons">
            <div className="ic">
              <img
                src="/media-kit/png/app-icon/nectar-app-icon-512.png"
                width={120}
                height={120}
                alt=""
              />
              <span>512</span>
            </div>
            <div className="ic">
              <img
                src="/media-kit/png/app-icon/nectar-app-icon-256.png"
                width={80}
                height={80}
                alt=""
              />
              <span>256</span>
            </div>
            <div className="ic">
              <img
                src="/media-kit/png/app-icon/nectar-app-icon-180.png"
                width={56}
                height={56}
                alt=""
              />
              <span>180 · apple-touch</span>
            </div>
            <div className="ic">
              <img
                src="/media-kit/png/app-icon/nectar-app-icon-192.png"
                width={40}
                height={40}
                alt=""
              />
              <span>192</span>
            </div>
          </div>
          <p className="note mt-18">
            Rounded tile on canvas dark, <b>iOS superellipse radius (~22%)</b>, mark
            inset ~17%. Ships at 1024 / 512 / 256 / 192 / 180.
          </p>
        </section>

        {/* 06 Don'ts */}
        <section>
          <div className="sec-head">
            <span className="n">06</span>
            <h2>Misuse</h2>
          </div>
          <div className="donts">
            <div className="dont">
              <div className="box">
                <img
                  src="/media-kit/svg/nectar-mark.svg"
                  style={{ transform: "scaleX(1.5)" }}
                  alt=""
                />
              </div>
              <div className="x">don&apos;t distort</div>
            </div>
            <div className="dont">
              <div className="box">
                <img
                  src="/media-kit/svg/nectar-mark.svg"
                  style={{ transform: "rotate(20deg)" }}
                  alt=""
                />
              </div>
              <div className="x">don&apos;t rotate</div>
            </div>
            <div className="dont">
              <div className="box">
                <img
                  src="/media-kit/svg/nectar-mark.svg"
                  style={{ filter: "hue-rotate(180deg) saturate(1.4)" }}
                  alt=""
                />
              </div>
              <div className="x">don&apos;t recolor</div>
            </div>
            <div className="dont">
              <div className="box" style={{ background: "#fff" }}>
                <img src="/media-kit/svg/nectar-mark.svg" alt="" />
              </div>
              <div className="x">don&apos;t place mint on white</div>
            </div>
            <div className="dont">
              <div
                className="box"
                style={{ background: "linear-gradient(135deg, #7c3aed, #db2777)" }}
              >
                <img src="/media-kit/svg/nectar-mark.svg" alt="" />
              </div>
              <div className="x">no gradients behind</div>
            </div>
          </div>
        </section>

        {/* 07 Assets */}
        <section>
          <div className="sec-head">
            <span className="n">07</span>
            <h2>Asset index</h2>
          </div>
          <table>
            <thead>
              <tr>
                <th style={{ width: "34%" }}>Asset</th>
                <th style={{ width: "42%" }}>Files</th>
                <th>Download</th>
              </tr>
            </thead>
            <tbody>
              {ASSETS.map((a) => (
                <tr key={a.name}>
                  <td>{a.name}</td>
                  <td className="files">{a.files}</td>
                  <td>
                    {a.svg && (
                      <a href={a.svg} download>
                        svg
                      </a>
                    )}
                    {a.png && (
                      <a href={a.png} download>
                        png
                      </a>
                    )}
                    {a.jpg && (
                      <a href={a.jpg} download>
                        jpg
                      </a>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      </div>
    </div>
  );
}
