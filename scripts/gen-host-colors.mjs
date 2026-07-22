// Generate the per-theme host-identity palette baked into internal/ui/static/
// app.css (the `--host-0…5` slots, multi-host fan-in / ADR-0048). Run with
// `node scripts/gen-host-colors.mjs` and paste the output into that block.
//
// The six hues are an evenly-spaced cool ramp (azure → magenta, ~190–320°) —
// the arc of the wheel each theme's status colours (warm / green / teal / red)
// leave free — so a host tint never reads as a deploy state, while still being
// as distinct as that arc allows. A full-wheel variant was more distinct but
// overlapped the status hues and read worse. Saturation follows each theme's
// mood; lightness suits its ground (bright on dark, darker on light).
const HUES = [190, 214, 240, 267, 294, 320];

function hslToHex(h, s, l) {
  s /= 100;
  l /= 100;
  const k = (n) => (n + h / 30) % 12;
  const a = s * Math.min(l, 1 - l);
  const f = (n) => l - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
  const to = (x) =>
    Math.round(255 * x)
      .toString(16)
      .padStart(2, '0');
  return `#${to(f(0))}${to(f(8))}${to(f(4))}`;
}

// Per theme: [darkS, darkL, lightS, lightL].
const themes = {
  catppuccin: [68, 74, 66, 46],
  nord: [42, 70, 42, 44],
  solarized: [52, 66, 62, 44],
  gruvbox: [48, 70, 55, 42],
  'rose-pine': [50, 74, 46, 47],
};

const line = (hexes) => hexes.map((c, i) => `--host-${i}:${c};`).join(' ');
for (const [name, [ds, dl, ls, ll]] of Object.entries(themes)) {
  console.log(`${name} dark : ${line(HUES.map((h) => hslToHex(h, ds, dl)))}`);
  console.log(`${name} light: ${line(HUES.map((h) => hslToHex(h, ls, ll)))}`);
}
