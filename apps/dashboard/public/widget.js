/**
 * Bridge Verify drop-in widget. A self-contained ES module with no dependencies.
 *
 *   <script type="module" src="https://YOUR-BRIDGE-DASHBOARD/widget.js"></script>
 *   <bridge-verify publishable-key="bpk_…" phone="+919876543210" label="Verify phone"></bridge-verify>
 *
 * Clicking the button opens a dialog that sends a code to the number and checks
 * it. Events on the element (they bubble and cross shadow roots):
 *   bridge-verified  detail: { token, phone, verificationId }
 *   bridge-error     detail: { code, message }
 *   bridge-cancel
 *
 * Or from code: window.BridgeVerify.open({ publishableKey, phone? })
 *   resolves to { token, phone, verificationId }, rejects when the dialog is closed.
 *
 * Send the token to your server and check it there (POST /v1/otp/tokens/verify,
 * or as an HS256 JWT with the app's signing secret). Never trust the phone
 * number from the browser alone.
 */

const ORIGIN = new URL(import.meta.url).origin;
const API = `${ORIGIN}/api/v1/widget/`;
const TURNSTILE_SRC = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
const KEY_PATTERN = /^bpk_[0-9A-Za-z]{32}$/;

// ---- Countries (ISO code and calling code; keep in sync with src/lib/countries.ts) ----

const RAW_COUNTRIES =
  'AC247 AD376 AE971 AF93 AG1 AI1 AL355 AM374 AO244 AR54 AS1 AT43 AU61 AW297 AX358 AZ994 ' +
  'BA387 BB1 BD880 BE32 BF226 BG359 BH973 BI257 BJ229 BL590 BM1 BN673 BO591 BQ599 BR55 BS1 ' +
  'BT975 BW267 BY375 BZ501 CA1 CC61 CD243 CF236 CG242 CH41 CI225 CK682 CL56 CM237 CN86 CO57 ' +
  'CR506 CU53 CV238 CW599 CX61 CY357 CZ420 DE49 DJ253 DK45 DM1 DO1 DZ213 EC593 EE372 EG20 ' +
  'EH212 ER291 ES34 ET251 FI358 FJ679 FK500 FM691 FO298 FR33 GA241 GB44 GD1 GE995 GF594 GG44 ' +
  'GH233 GI350 GL299 GM220 GN224 GP590 GQ240 GR30 GT502 GU1 GW245 GY592 HK852 HN504 HR385 ' +
  'HT509 HU36 ID62 IE353 IL972 IM44 IN91 IO246 IQ964 IR98 IS354 IT39 JE44 JM1 JO962 JP81 ' +
  'KE254 KG996 KH855 KI686 KM269 KN1 KP850 KR82 KW965 KY1 KZ7 LA856 LB961 LC1 LI423 LK94 ' +
  'LR231 LS266 LT370 LU352 LV371 LY218 MA212 MC377 MD373 ME382 MF590 MG261 MH692 MK389 ML223 ' +
  'MM95 MN976 MO853 MP1 MQ596 MR222 MS1 MT356 MU230 MV960 MW265 MX52 MY60 MZ258 NA264 NC687 ' +
  'NE227 NF672 NG234 NI505 NL31 NO47 NP977 NR674 NU683 NZ64 OM968 PA507 PE51 PF689 PG675 PH63 ' +
  'PK92 PL48 PM508 PR1 PS970 PT351 PW680 PY595 QA974 RE262 RO40 RS381 RU7 RW250 SA966 SB677 ' +
  'SC248 SD249 SE46 SG65 SH290 SI386 SJ47 SK421 SL232 SM378 SN221 SO252 SR597 SS211 ST239 ' +
  'SV503 SX1 SY963 SZ268 TA290 TC1 TD235 TG228 TH66 TJ992 TK690 TL670 TM993 TN216 TO676 TR90 ' +
  'TT1 TV688 TW886 TZ255 UA380 UG256 US1 UY598 UZ998 VA39 VC1 VE58 VG1 VI1 VN84 VU678 WF681 ' +
  'WS685 XK383 YE967 YT262 ZA27 ZM260 ZW263';

/** Countries that own a shared calling code when a number does not say otherwise. */
const MAIN_FOR_DIAL = ['US', 'RU', 'GB', 'FI', 'NO', 'IT', 'MA', 'FR', 'AU'];

let countries = null;
function allCountries() {
  if (countries) return countries;
  let names = null;
  try {
    names = new Intl.DisplayNames([navigator.language, 'en'], { type: 'region' });
  } catch {
    names = null;
  }
  countries = RAW_COUNTRIES.split(' ')
    .map((s) => {
      const code = s.slice(0, 2);
      let name = code;
      try {
        name = names?.of(code) ?? code;
      } catch {
        name = code;
      }
      return { code, dial: s.slice(2), name };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
  return countries;
}

function countryByCode(code) {
  return allCountries().find((c) => c.code === code);
}

function defaultCountry() {
  const tags = navigator.languages?.length ? navigator.languages : [navigator.language];
  for (const tag of tags) {
    try {
      const region = new Intl.Locale(tag).maximize().region;
      if (region && countryByCode(region)) return region;
    } catch {
      // ignore malformed tags
    }
  }
  return 'US';
}

function toE164(input, country) {
  const raw = input.trim();
  let digits = raw.replace(/\D/g, '');
  if (raw.startsWith('+')) return validE164(digits);
  if (digits.startsWith('00')) return validE164(digits.slice(2));
  const c = countryByCode(country);
  if (!c) return null;
  // Most countries write a trunk 0 before national numbers; Italy keeps it.
  if (!['IT', 'VA', 'SM'].includes(c.code)) digits = digits.replace(/^0/, '');
  return validE164(c.dial + digits);
}

function validE164(digits) {
  return /^[1-9]\d{6,14}$/.test(digits) ? `+${digits}` : null;
}

/** Splits +<digits> into a country and the national part, preferring `preferred`. */
function splitE164(value, preferred) {
  const raw = (value || '').trim();
  if (!raw.startsWith('+')) return null;
  const digits = raw.replace(/\D/g, '');
  for (let len = 4; len >= 1; len--) {
    const dial = digits.slice(0, len);
    const matches = allCountries().filter((c) => c.dial === dial);
    if (!matches.length) continue;
    const pick =
      matches.find((c) => c.code === preferred) ??
      matches.find((c) => MAIN_FOR_DIAL.includes(c.code)) ??
      matches[0];
    return { country: pick.code, national: digits.slice(len) };
  }
  return null;
}

// ---- API -----------------------------------------------------------------------

class WidgetError extends Error {
  constructor(status, code, message, retryAfter) {
    super(message);
    this.name = 'WidgetError';
    this.status = status;
    this.code = code;
    this.retryAfter = retryAfter;
  }
}

async function call(key, path, body) {
  let res;
  try {
    res = await fetch(API + encodeURIComponent(key) + path, {
      method: body ? 'POST' : 'GET',
      credentials: 'omit',
      cache: 'no-store',
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new WidgetError(
      0,
      'network_error',
      `Could not reach ${ORIGIN}. Check the connection, and that this site's origin is in the app's allowed origins.`,
      null,
    );
  }
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    const retry = Number(res.headers.get('retry-after'));
    throw new WidgetError(
      res.status,
      data?.error?.code ?? 'internal_error',
      data?.error?.message ?? `Bridge returned ${res.status}.`,
      Number.isFinite(retry) && retry > 0 ? retry : null,
    );
  }
  return data;
}

function waitText(seconds) {
  if (seconds < 60) return `${seconds} second${seconds === 1 ? '' : 's'}`;
  const minutes = Math.ceil(seconds / 60);
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? '' : 's'}`;
  const hours = Math.ceil(minutes / 60);
  return `${hours} hour${hours === 1 ? '' : 's'}`;
}

/** What to tell the person verifying their number. */
function errorText(err, step) {
  if (!(err instanceof WidgetError)) return 'Something went wrong. Try again.';
  const wait = err.retryAfter ? ` Try again in ${waitText(err.retryAfter)}.` : ' Try again later.';
  if (err.status === 0) {
    return 'Could not reach the verification service. Check your connection and try again.';
  }
  if (err.code === 'otp_blocked' && err.status === 403) {
    return /captcha/i.test(err.message)
      ? 'The security check did not pass. Try again.'
      : 'Codes cannot be sent to numbers in this country. Use a different number.';
  }
  if (err.code === 'otp_blocked') return `Too many codes were requested.${wait}`;
  if (err.status === 429) {
    return step === 'send'
      ? `Please wait before asking for another code.${wait}`
      : `Too many tries.${wait}`;
  }
  if (err.status === 503) {
    return 'The security check is not available right now. Try again in a moment.';
  }
  if (err.status === 404 && step === 'verify')
    return 'This code is no longer valid. Send a new one.';
  if (err.status === 404) return 'This verification widget is not set up correctly.';
  if (err.status === 403) return 'This site is not allowed to use this verification widget.';
  if (err.status === 409) return 'Verification is not available for this app right now.';
  if (err.status === 400 || err.status === 422) {
    return step === 'send'
      ? 'Check the phone number and try again.'
      : 'Check the code and try again.';
  }
  return 'Something went wrong. Try again.';
}

// ---- Turnstile -------------------------------------------------------------------

let turnstileLoading = null;
function loadTurnstile() {
  if (window.turnstile) return Promise.resolve(window.turnstile);
  turnstileLoading ??= new Promise((resolve, reject) => {
    const s = document.createElement('script');
    s.src = TURNSTILE_SRC;
    s.async = true;
    s.onload = () =>
      window.turnstile ? resolve(window.turnstile) : reject(new Error('turnstile'));
    s.onerror = () => {
      turnstileLoading = null;
      reject(new Error('turnstile'));
    };
    document.head.appendChild(s);
  });
  return turnstileLoading;
}

// ---- DOM helpers -------------------------------------------------------------------

/** Creates an element. Text children are set as text, never parsed as HTML. */
function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === false || v === null || v === undefined) continue;
    if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'text') el.textContent = v;
    else el.setAttribute(k, v === true ? '' : String(v));
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

const SVG_NS = 'http://www.w3.org/2000/svg';
function icon(d, size = 16, stroke = 2) {
  const svg = document.createElementNS(SVG_NS, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('fill', 'none');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('stroke-width', String(stroke));
  svg.setAttribute('stroke-linecap', 'round');
  svg.setAttribute('stroke-linejoin', 'round');
  svg.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS(SVG_NS, 'path');
  path.setAttribute('d', d);
  svg.append(path);
  return svg;
}
const ICON_CLOSE = 'M18 6 6 18M6 6l12 12';
const ICON_MARK = 'M6.5 20V5M17.5 20V5M2 15.5h20M2.5 10 6.5 5c2.5 6 8.5 6 11 0l4 5';
const ICON_CHECK = 'M20 6 9 17l-5-5';

const TOKENS = `
  :host, .bv {
    --bv-bg: #faf8f4; --bv-card: #ffffff; --bv-fg: #1a1714; --bv-muted: #5e564c;
    --bv-border: #e5e0d8; --bv-input: #dcd6cc; --bv-field: #fbfaf7;
    --bv-primary: #0f7a62; --bv-primary-fg: #ffffff;
    --bv-danger: #dc2626; --bv-warning: #b45309; --bv-success: #15803d;
    --bv-ring: rgb(15 122 98 / 0.35);
  }
  @media (prefers-color-scheme: dark) {
    :host, .bv {
      --bv-bg: #0c0b0a; --bv-card: #14120f; --bv-fg: #f7f4ef; --bv-muted: #a8a097;
      --bv-border: #2b2722; --bv-input: #34302a; --bv-field: #1a1814;
      --bv-primary: #3eebc0; --bv-primary-fg: #0a0a0b;
      --bv-danger: #f87171; --bv-warning: #fbbf24; --bv-success: #4ade80;
      --bv-ring: rgb(62 235 192 / 0.35);
    }
  }
`;

const BUTTON_CSS = `${TOKENS}
  :host { display: inline-block; }
  :host([hidden]) { display: none; }
  button {
    font: inherit; font-weight: 600; font-size: 0.95em; line-height: 1;
    display: inline-flex; align-items: center; gap: 0.5em;
    padding: 0.75em 1.1em; border-radius: 0.6em; border: 1px solid transparent;
    background: var(--bv-primary); color: var(--bv-primary-fg); cursor: pointer;
    transition: filter 120ms ease;
  }
  button:hover { filter: brightness(0.94); }
  button:focus-visible { outline: none; box-shadow: 0 0 0 3px var(--bv-ring); }
  button:disabled { opacity: 0.6; cursor: default; }
`;

const DIALOG_CSS = `${TOKENS}
  * { box-sizing: border-box; }
  [hidden] { display: none !important; }
  .bv {
    font-family: system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
    font-size: 15px; line-height: 1.5; color: var(--bv-fg);
    padding: 0; border: none; background: transparent; overflow: visible;
    width: 400px; max-width: calc(100vw - 32px); max-height: calc(100dvh - 32px);
  }
  .bv::backdrop { background: rgb(12 11 10 / 0.55); }
  .panel {
    background: var(--bv-card); border: 1px solid var(--bv-border); border-radius: 16px;
    padding: 24px; box-shadow: 0 24px 48px -12px rgb(0 0 0 / 0.35);
    max-height: calc(100dvh - 32px); overflow-y: auto;
  }
  @media (prefers-reduced-motion: no-preference) {
    .bv[open] .panel { animation: bv-in 160ms ease-out; }
    @keyframes bv-in { from { opacity: 0; transform: translateY(6px) scale(0.99); } }
  }
  @media (max-width: 480px) {
    .bv { width: 100%; max-width: 100%; margin: auto 0 0; max-height: 100dvh; }
    .panel {
      border-radius: 16px 16px 0 0; border-bottom: none;
      padding-bottom: calc(24px + env(safe-area-inset-bottom));
    }
  }
  .head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 20px; }
  .brand { display: flex; align-items: center; gap: 12px; min-width: 0; }
  .avatar {
    flex: none; display: grid; place-items: center; width: 40px; height: 40px; border-radius: 12px;
    background: color-mix(in srgb, var(--bv-primary) 13%, transparent); color: var(--bv-primary);
    font-weight: 700; font-size: 17px;
  }
  .app {
    margin: 0; font-size: 11px; font-weight: 600; letter-spacing: 0.14em; text-transform: uppercase;
    color: var(--bv-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  h2 { margin: 0; font-size: 20px; line-height: 1.25; font-weight: 650; letter-spacing: -0.01em; }
  .close {
    flex: none; display: grid; place-items: center; width: 32px; height: 32px; margin: -4px -6px 0 0;
    border: none; border-radius: 8px; background: transparent; color: var(--bv-muted); cursor: pointer;
  }
  .close:hover { background: var(--bv-field); color: var(--bv-fg); }
  p { margin: 0; }
  .lead { color: var(--bv-muted); font-size: 14px; margin-bottom: 16px; }
  .banner {
    font-size: 13px; padding: 8px 12px; border-radius: 10px; margin-bottom: 16px;
    border: 1px solid color-mix(in srgb, var(--bv-warning) 40%, transparent);
    background: color-mix(in srgb, var(--bv-warning) 8%, transparent);
  }
  .banner b { color: var(--bv-warning); }
  .code-chip { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-weight: 700; letter-spacing: 0.2em; margin-left: 4px; }
  form { display: flex; flex-direction: column; gap: 14px; margin: 0; }
  label { font-size: 13px; font-weight: 600; display: block; margin-bottom: 6px; }
  .row { display: flex; gap: 8px; }
  .field {
    font: inherit; font-size: 16px; color: var(--bv-fg); background: var(--bv-field);
    border: 1px solid var(--bv-input); border-radius: 10px; height: 46px; padding: 0 12px; width: 100%; min-width: 0;
  }
  .field::placeholder { color: var(--bv-muted); opacity: 0.8; }
  .field.mono:not(.otp)::placeholder { font-family: system-ui, sans-serif; }
  .field[aria-invalid="true"] { border-color: var(--bv-danger); }
  .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
  .otp { text-align: center; font-size: 22px; letter-spacing: 0.4em; height: 52px; }
  .country { position: relative; flex: none; border-radius: 10px; }
  .country .face {
    display: flex; align-items: center; gap: 6px; height: 46px; padding: 0 12px;
    border: 1px solid var(--bv-input); border-radius: 10px; background: var(--bv-field);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 14px;
  }
  .country .dial { color: var(--bv-muted); }
  .country select { position: absolute; inset: 0; opacity: 0; cursor: pointer; width: 100%; font-size: 16px; }
  .country:has(select:focus-visible) .face { border-color: var(--bv-primary); box-shadow: 0 0 0 3px var(--bv-ring); }
  .field:focus-visible { outline: none; border-color: var(--bv-primary); box-shadow: 0 0 0 3px var(--bv-ring); }
  .primary {
    font: inherit; font-size: 15px; font-weight: 600; height: 46px; width: 100%; border-radius: 10px;
    border: none; background: var(--bv-primary); color: var(--bv-primary-fg); cursor: pointer;
  }
  .primary:hover { filter: brightness(0.94); }
  .primary:disabled { opacity: 0.55; cursor: default; filter: none; }
  .link {
    font: inherit; font-weight: 600; color: var(--bv-primary); background: none; border: none; padding: 0;
    cursor: pointer; border-radius: 4px; text-underline-offset: 4px;
  }
  .link:hover { text-decoration: underline; }
  .link:disabled { opacity: 0.5; cursor: default; text-decoration: none; }
  .close:focus-visible, .link:focus-visible, .primary:focus-visible {
    outline: none; box-shadow: 0 0 0 3px var(--bv-ring);
  }
  .error { color: var(--bv-danger); font-size: 14px; }
  .muted { color: var(--bv-muted); }
  .center { text-align: center; font-size: 14px; }
  .done { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 12px 0; text-align: center; }
  .done .tick {
    display: grid; place-items: center; width: 44px; height: 44px; border-radius: 50%;
    background: color-mix(in srgb, var(--bv-success) 14%, transparent); color: var(--bv-success);
  }
  .done strong { font-size: 17px; }
  .skeleton { height: 46px; border-radius: 10px; background: var(--bv-field); margin-bottom: 12px; }
  .ts { margin-top: 12px; }
  .foot {
    display: flex; align-items: center; justify-content: flex-end; gap: 6px; margin-top: 20px;
    font-size: 12px; color: var(--bv-muted);
  }
  .sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
`;

// ---- The dialog ----------------------------------------------------------------------

let seq = 0;

/**
 * One verification. Lives in its own shadow root on <body>, as a native modal
 * <dialog>: the page behind is inert, Esc closes it, and focus returns to
 * where it was.
 */
class VerifyDialog {
  constructor({ publishableKey, phone, emit }) {
    this.key = publishableKey;
    this.phone = phone || '';
    this.emit = emit || (() => {});
    this.uid = `bv${++seq}`;
    this.config = null;
    this.finished = false;
    this.lastError = null;
    this.timer = null;
    this.tsWidget = null;
    this.tsToken = null;
    this.tsWaiters = [];
    this.returnFocus = document.activeElement;
    this.promise = new Promise((resolve, reject) => {
      this.resolve = resolve;
      this.reject = reject;
    });
  }

  open() {
    this.host = h('div', { 'data-bridge-verify': '' });
    this.tsBox = h('div', { slot: 'turnstile' });
    this.host.append(this.tsBox);
    const root = this.host.attachShadow({ mode: 'open' });
    this.root = root;
    const style = h('style', { text: DIALOG_CSS });

    this.appLabel = h('p', { class: 'app' });
    this.avatar = h('span', { class: 'avatar', 'aria-hidden': 'true' });
    this.avatar.hidden = true;
    this.banner = h(
      'p',
      { class: 'banner', hidden: true },
      h('b', { text: 'Test mode.' }),
      ' No SMS is sent. The code is shown on the next step.',
    );
    this.body = h('div', { 'aria-live': 'polite' });
    this.dialog = h(
      'dialog',
      { class: 'bv', 'aria-labelledby': `${this.uid}-title` },
      h(
        'div',
        { class: 'panel' },
        h(
          'div',
          { class: 'head' },
          h(
            'div',
            { class: 'brand' },
            this.avatar,
            h(
              'div',
              {},
              this.appLabel,
              h('h2', { id: `${this.uid}-title`, text: 'Verify your phone' }),
            ),
          ),
          h(
            'button',
            { class: 'close', type: 'button', 'aria-label': 'Close', onclick: () => this.cancel() },
            icon(ICON_CLOSE, 18),
          ),
        ),
        this.banner,
        this.body,
        h('div', { class: 'ts' }, h('slot', { name: 'turnstile' })),
        h('div', { class: 'foot' }, icon(ICON_MARK, 14, 2.2), 'Secured by Bridge'),
      ),
    );
    this.dialog.addEventListener('cancel', (e) => {
      e.preventDefault();
      this.cancel();
    });
    this.dialog.addEventListener('keydown', (e) => this.trapFocus(e));
    root.append(style, this.dialog);
    document.body.append(this.host);
    this.dialog.showModal();
    this.loading();
    this.load();
    return this.promise;
  }

  /** Keeps Tab and Shift+Tab inside the dialog. */
  trapFocus(e) {
    if (e.key !== 'Tab') return;
    const items = [
      ...this.dialog.querySelectorAll('button, input, select, [tabindex]:not([tabindex="-1"])'),
    ].filter((el) => !el.disabled && el.offsetParent !== null);
    if (!items.length) return;
    const first = items[0];
    const last = items[items.length - 1];
    const active = this.root.activeElement;
    if (e.shiftKey && (active === first || !active)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  }

  async load() {
    if (!KEY_PATTERN.test(this.key)) {
      this.fatal(
        'invalid_publishable_key',
        `"${this.key}" is not a Bridge publishable key (bpk_ followed by 32 letters and digits).`,
        'This verification widget is not set up correctly.',
      );
      return;
    }
    try {
      this.config = await call(this.key, '');
    } catch (err) {
      // A refused origin gets no CORS headers, so the browser reports it as a network error.
      const text =
        err.status === 0
          ? 'Verification is not available on this page right now.'
          : errorText(err, 'load');
      this.fatal(err.code || 'internal_error', err.message, text);
      return;
    }
    const name = this.config.app_name || '';
    this.appLabel.textContent = name;
    this.avatar.textContent = name.trim().charAt(0).toUpperCase();
    this.avatar.hidden = !name;
    this.banner.hidden = this.config.environment !== 'test';
    if (this.config.turnstile_site_key) this.startTurnstile();
    this.phoneStep();
  }

  loading() {
    this.body.replaceChildren(
      h('span', { class: 'sr', text: 'Loading' }),
      h('div', { class: 'skeleton' }),
      h('div', { class: 'skeleton' }),
    );
  }

  /** Shows an error that ends the flow, and reports it to the page. */
  fatal(code, message, text) {
    this.lastError = { code, message };
    this.emit('bridge-error', { code, message });
    console.warn(`[Bridge Verify] ${message}`);
    this.body.replaceChildren(
      h('p', { class: 'error', role: 'alert', text }),
      h(
        'button',
        {
          class: 'primary',
          type: 'button',
          style: 'margin-top:16px',
          onclick: () => this.cancel(),
        },
        'Close',
      ),
    );
    this.body.querySelector('button')?.focus();
  }

  report(err) {
    if (err instanceof WidgetError) {
      this.lastError = { code: err.code, message: err.message };
      this.emit('bridge-error', { code: err.code, message: err.message });
    }
  }

  // -- Turnstile, rendered in the page (slotted) so Cloudflare's iframe works normally.

  startTurnstile() {
    loadTurnstile()
      .then((ts) => {
        if (this.finished) return;
        this.tsWidget = ts.render(this.tsBox, {
          sitekey: this.config.turnstile_site_key,
          appearance: 'interaction-only',
          theme: 'auto',
          size: 'flexible',
          callback: (t) => {
            const w = this.tsWaiters.shift();
            if (w) w(t);
            else this.tsToken = t;
          },
          'expired-callback': () => {
            this.tsToken = null;
          },
          'error-callback': () => {
            this.emit('bridge-error', {
              code: 'captcha_error',
              message: 'Cloudflare Turnstile could not run.',
            });
            return true;
          },
        });
      })
      .catch(() => {
        const message =
          'Cloudflare Turnstile could not load. Allow https://challenges.cloudflare.com in your Content-Security-Policy (script-src and frame-src).';
        this.lastError = { code: 'captcha_unavailable', message };
        this.emit('bridge-error', { code: 'captcha_unavailable', message });
      });
  }

  turnstileToken() {
    if (!this.config?.turnstile_site_key) return Promise.resolve(undefined);
    if (this.tsToken) {
      const t = this.tsToken;
      this.tsToken = null;
      return Promise.resolve(t);
    }
    return new Promise((resolve, reject) => {
      const done = (t) => {
        clearTimeout(timer);
        resolve(t);
      };
      const timer = setTimeout(() => {
        this.tsWaiters = this.tsWaiters.filter((w) => w !== done);
        reject(new Error('turnstile_timeout'));
      }, 60_000);
      this.tsWaiters.push(done);
    });
  }

  nextTurnstile() {
    this.tsToken = null;
    if (this.tsWidget !== null) window.turnstile?.reset(this.tsWidget);
  }

  async send(to, setBusy) {
    try {
      setBusy(this.config.turnstile_site_key ? 'Checking your browser…' : 'Sending code…');
      const token = await this.turnstileToken();
      setBusy('Sending code…');
      const sent = await call(this.key, '/send', { to, turnstile_token: token });
      this.codeStep(to, sent);
      return null;
    } catch (err) {
      if (err instanceof Error && err.message === 'turnstile_timeout') {
        return 'The security check did not finish. Try again.';
      }
      this.report(err);
      return errorText(err, 'send');
    } finally {
      this.nextTurnstile();
    }
  }

  // -- Steps

  phoneStep() {
    const split = splitE164(this.phone, defaultCountry());
    let country = split?.country ?? defaultCountry();
    const id = this.uid;
    const face = h('span', { class: 'face', 'aria-hidden': 'true' });
    const paintFace = () => {
      face.replaceChildren(
        h('span', { text: country }),
        h('span', { class: 'dial', text: `+${countryByCode(country)?.dial ?? ''}` }),
        icon('m6 9 6 6 6-6', 14),
      );
    };
    paintFace();
    const select = h(
      'select',
      {
        'aria-label': 'Country',
        onchange: (e) => {
          country = e.target.value;
          paintFace();
          setError('');
        },
      },
      allCountries().map((c) =>
        h('option', { value: c.code, selected: c.code === country }, `${c.name} (+${c.dial})`),
      ),
    );
    const input = h('input', {
      id: `${id}-phone`,
      class: 'field mono',
      type: 'tel',
      inputmode: 'tel',
      autocomplete: 'tel',
      placeholder: 'Phone number',
      value: split?.national ?? (this.phone.startsWith('+') ? '' : this.phone),
      'aria-describedby': `${id}-phone-error`,
    });
    input.addEventListener('input', () => {
      setError('');
      const v = input.value.trim();
      if (!v.startsWith('+')) return;
      const s = splitE164(v, country);
      if (s && s.national.length >= 4) {
        country = s.country;
        select.value = country;
        paintFace();
        input.value = s.national;
      }
    });
    const error = h('p', { id: `${id}-phone-error`, class: 'error', role: 'alert', hidden: true });
    const submit = h('button', { class: 'primary', type: 'submit' }, 'Send code');
    const setError = (text) => {
      error.textContent = text;
      error.hidden = !text;
      input.setAttribute('aria-invalid', text ? 'true' : 'false');
    };
    const setBusy = (text) => {
      submit.disabled = Boolean(text);
      submit.textContent = text || 'Send code';
    };
    const form = h(
      'form',
      {
        novalidate: true,
        onsubmit: async (e) => {
          e.preventDefault();
          const to = toE164(input.value, country);
          if (!to) {
            setError('Enter a valid phone number, including the area code.');
            input.focus();
            return;
          }
          setError('');
          const problem = await this.send(to, setBusy);
          if (problem) {
            setBusy('');
            setError(problem);
          }
        },
      },
      h(
        'div',
        {},
        h('label', { for: `${id}-phone`, text: 'Phone number' }),
        h('div', { class: 'row' }, h('span', { class: 'country' }, face, select), input),
      ),
      error,
      submit,
    );
    this.body.replaceChildren(
      h('p', { class: 'lead', text: 'We will text a code to this number to confirm it is yours.' }),
      form,
    );
    input.focus();
  }

  codeStep(phone, sent) {
    clearInterval(this.timer);
    const id = this.uid;
    const length = this.config.code_length;
    let dead = false;
    let lastTried = '';
    let checking = false;

    const input = h('input', {
      id: `${id}-code`,
      class: 'field mono otp',
      inputmode: 'numeric',
      autocomplete: 'one-time-code',
      pattern: `\\d{${length}}`,
      maxlength: length,
      placeholder: '•'.repeat(length),
      'aria-describedby': `${id}-code-error`,
    });
    const error = h('p', { id: `${id}-code-error`, class: 'error', role: 'alert', hidden: true });
    const submit = h('button', { class: 'primary', type: 'submit', disabled: true }, 'Verify');
    const resendSlot = h('p', { class: 'center' });
    const setError = (text) => {
      error.textContent = text;
      error.hidden = !text;
      input.setAttribute('aria-invalid', text ? 'true' : 'false');
    };

    const check = async (value) => {
      if (checking || dead || value.length !== length) return;
      checking = true;
      lastTried = value;
      submit.disabled = true;
      submit.textContent = 'Checking…';
      setError('');
      try {
        const r = await call(this.key, '/verify', {
          verification_id: sent.verification_id,
          code: value,
        });
        if (r.valid && r.token) {
          this.success({ token: r.token, phone, verificationId: sent.verification_id });
          return;
        }
        if (r.status === 'pending') {
          setError(
            `That code is not right. ${r.attempts_remaining} attempt${r.attempts_remaining === 1 ? '' : 's'} left.`,
          );
          input.value = '';
          input.focus();
        } else {
          dead = true;
          input.disabled = true;
          setError(
            r.status === 'expired'
              ? 'This code has expired. Send a new one.'
              : r.status === 'failed'
                ? 'Too many wrong codes. Send a new one.'
                : 'A newer code was sent. Use the latest one, or send a new one.',
          );
        }
      } catch (err) {
        this.report(err);
        setError(errorText(err, 'verify'));
      } finally {
        checking = false;
        submit.textContent = 'Verify';
        submit.disabled = dead || input.value.length !== length;
      }
    };

    input.addEventListener('input', () => {
      const digits = input.value.replace(/\D/g, '').slice(0, length);
      input.value = digits;
      setError('');
      submit.disabled = dead || digits.length !== length;
      if (digits.length === length && digits !== lastTried) check(digits);
    });

    const resendAt = new Date(sent.resend_available_at).getTime();
    const paintResend = () => {
      const wait = Math.max(0, Math.ceil((resendAt - Date.now()) / 1000));
      if (wait > 0) {
        resendSlot.replaceChildren(
          h('span', { class: 'muted', text: `Send a new code in ${waitText(wait)}` }),
        );
        return;
      }
      clearInterval(this.timer);
      const btn = h(
        'button',
        {
          class: 'link',
          type: 'button',
          onclick: async () => {
            const problem = await this.send(phone, (text) => {
              btn.disabled = Boolean(text);
              btn.textContent = text ? 'Sending…' : 'Send a new code';
            });
            if (problem) {
              btn.disabled = false;
              btn.textContent = 'Send a new code';
              setError(problem);
            }
          },
        },
        'Send a new code',
      );
      resendSlot.replaceChildren(btn);
    };
    paintResend();
    this.timer = setInterval(paintResend, 1000);

    const lead = h(
      'p',
      { class: 'lead' },
      `Enter the ${length}-digit code sent to `,
      h('span', { class: 'mono', style: 'color:var(--bv-fg);white-space:nowrap', text: phone }),
      '. ',
      h(
        'button',
        {
          class: 'link',
          type: 'button',
          onclick: () => {
            clearInterval(this.timer);
            this.phone = phone;
            this.phoneStep();
          },
        },
        'Change number',
      ),
    );
    const testCode = sent.code
      ? h('p', { class: 'banner' }, 'Test code', h('span', { class: 'code-chip', text: sent.code }))
      : null;
    const form = h(
      'form',
      {
        novalidate: true,
        onsubmit: (e) => {
          e.preventDefault();
          lastTried = '';
          check(input.value);
        },
      },
      h('div', {}, h('label', { for: `${id}-code`, text: 'Code' }), input),
      error,
      submit,
      resendSlot,
    );
    this.body.replaceChildren(lead, testCode, form);
    input.focus();
  }

  success(result) {
    this.finished = true;
    clearInterval(this.timer);
    this.body.replaceChildren(
      h(
        'div',
        { class: 'done', role: 'status' },
        h('span', { class: 'tick' }, icon(ICON_CHECK, 22, 2.4)),
        h('strong', { text: 'Phone number verified' }),
      ),
    );
    this.emit('bridge-verified', result);
    this.resolve(result);
    setTimeout(() => this.teardown(), 900);
  }

  cancel() {
    if (this.finished) return;
    this.finished = true;
    clearInterval(this.timer);
    this.emit('bridge-cancel', null);
    const err = new Error(this.lastError?.message ?? 'The verification was cancelled.');
    err.name = 'BridgeVerifyError';
    err.code = this.lastError?.code ?? 'cancelled';
    this.reject(err);
    this.teardown();
  }

  teardown() {
    this.finished = true;
    clearInterval(this.timer);
    if (this.tsWidget !== null) window.turnstile?.remove(this.tsWidget);
    this.tsWidget = null;
    if (this.dialog.open) this.dialog.close();
    this.host.remove();
    if (this.returnFocus instanceof HTMLElement && this.returnFocus.isConnected) {
      this.returnFocus.focus();
    }
    active = null;
  }
}

let active = null;

function open({ publishableKey, phone, emit } = {}) {
  if (active) return active.promise;
  active = new VerifyDialog({ publishableKey: (publishableKey || '').trim(), phone, emit });
  return active.open();
}

// ---- <bridge-verify> -------------------------------------------------------------------

class BridgeVerifyElement extends HTMLElement {
  static get observedAttributes() {
    return ['label'];
  }

  connectedCallback() {
    if (this.shadowRoot) return;
    const root = this.attachShadow({ mode: 'open' });
    this.button = h(
      'button',
      { type: 'button', part: 'button', onclick: () => this.open().catch(() => {}) },
      icon(ICON_MARK, 16, 2.2),
      h('span', { text: this.getAttribute('label') || 'Verify phone number' }),
    );
    root.append(h('style', { text: BUTTON_CSS }), this.button);
  }

  attributeChangedCallback() {
    const span = this.button?.querySelector('span');
    if (span) span.textContent = this.getAttribute('label') || 'Verify phone number';
  }

  /** Opens the dialog. Resolves with { token, phone, verificationId }; rejects when closed. */
  open() {
    return open({
      publishableKey: this.getAttribute('publishable-key'),
      phone: this.getAttribute('phone'),
      emit: (type, detail) =>
        this.dispatchEvent(new CustomEvent(type, { detail, bubbles: true, composed: true })),
    });
  }
}

if (!customElements.get('bridge-verify')) {
  customElements.define('bridge-verify', BridgeVerifyElement);
}

export const BridgeVerify = {
  open: ({ publishableKey, phone } = {}) => open({ publishableKey, phone }),
};
window.BridgeVerify = BridgeVerify;
