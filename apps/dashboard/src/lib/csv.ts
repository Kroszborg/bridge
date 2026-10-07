/**
 * A small RFC 4180 CSV reader for recipient lists. Handles a UTF-8 byte order
 * mark, CRLF, LF or CR line endings, quoted fields with "" escapes and line
 * breaks inside quotes, and comma, semicolon or tab separators (spreadsheet
 * apps in many locales export with semicolons).
 */

export type CsvTable = { headers: string[]; rows: string[][]; delimiter: string };

function detectDelimiter(text: string): string {
  // Count candidate separators on the first line, outside quotes.
  const counts: Record<string, number> = { ',': 0, ';': 0, '\t': 0 };
  let quoted = false;
  for (const ch of text) {
    if (ch === '"') quoted = !quoted;
    else if (!quoted && (ch === '\n' || ch === '\r')) break;
    else if (!quoted && ch in counts) counts[ch] = (counts[ch] ?? 0) + 1;
  }
  let best = ',';
  for (const d of [';', '\t']) {
    if ((counts[d] ?? 0) > (counts[best] ?? 0)) best = d;
  }
  return best;
}

/** Splits CSV text into rows of fields. Blank lines are dropped. */
export function parseCsvRows(input: string, delimiter?: string): string[][] {
  const text = input.charCodeAt(0) === 0xfeff ? input.slice(1) : input;
  const sep = delimiter ?? detectDelimiter(text);
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let quoted = false;
  let i = 0;

  const endField = () => {
    row.push(field);
    field = '';
  };
  const endRow = () => {
    endField();
    if (row.some((f) => f.trim() !== '')) rows.push(row);
    row = [];
  };

  while (i < text.length) {
    const ch = text[i] as string;
    if (quoted) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i += 2;
          continue;
        }
        quoted = false;
        i += 1;
        continue;
      }
      field += ch;
      i += 1;
      continue;
    }
    if (ch === '"' && field.trim() === '') {
      // An opening quote; whitespace before it is ignored.
      field = '';
      quoted = true;
    } else if (ch === sep) {
      endField();
    } else if (ch === '\r') {
      endRow();
      if (text[i + 1] === '\n') i += 1;
    } else if (ch === '\n') {
      endRow();
    } else {
      field += ch;
    }
    i += 1;
  }
  if (field !== '' || row.length > 0) endRow();
  return rows;
}

/** Parses CSV with a header row. Short rows are padded so every row has a value per header. */
export function parseCsv(input: string): CsvTable {
  const text = input.charCodeAt(0) === 0xfeff ? input.slice(1) : input;
  const delimiter = detectDelimiter(text);
  const all = parseCsvRows(text, delimiter);
  const [first, ...rest] = all;
  const headers = (first ?? []).map((h, i) => h.trim() || `column_${i + 1}`);
  const rows = rest.map((r) => {
    const out = r.map((v) => v.trim());
    while (out.length < headers.length) out.push('');
    return out;
  });
  return { headers, rows, delimiter };
}

// ---- templates ------------------------------------------------------------

/** Mirrors the API's {placeholder} grammar (apps/api/internal/broadcast/template.go). */
const PLACEHOLDER = /\{([A-Za-z_][A-Za-z0-9_]{0,39})\}/g;

/** Distinct placeholder names in a template, in order of first use. */
export function placeholders(template: string): string[] {
  const seen = new Set<string>();
  for (const m of template.matchAll(PLACEHOLDER)) seen.add(m[1] as string);
  return [...seen];
}

/** Fills placeholders; missing values are left as they are. */
export function renderTemplate(template: string, vars: Record<string, string>): string {
  return template.replace(PLACEHOLDER, (whole, name: string) =>
    Object.hasOwn(vars, name) ? (vars[name] as string) : whole,
  );
}

/** A column header as a placeholder name: "First name" becomes first_name. */
export function variableName(header: string): string {
  let name = header
    .trim()
    .replace(/[^A-Za-z0-9_]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .toLowerCase();
  if (!name) name = 'column';
  if (/^[0-9]/.test(name)) name = `_${name}`;
  return name.slice(0, 40);
}

// ---- phone numbers ----------------------------------------------------------

/**
 * Normalises a number the way the API does before its own check: removes
 * spaces, dashes, dots and brackets, and requires a leading +. `defaultCode`
 * (like "+91") is added to numbers written without one.
 */
export function normalisePhone(
  raw: string,
  defaultCode = '',
): { ok: true; value: string } | { ok: false; reason: string } {
  let s = raw.trim().replace(/[\s\-().]/g, '');
  if (s === '') return { ok: false, reason: 'No number' };
  if (s.startsWith('00')) s = `+${s.slice(2)}`;
  if (!s.startsWith('+')) {
    const code = defaultCode.trim().replace(/[^\d+]/g, '');
    if (!code) return { ok: false, reason: 'No country code (add one like +91)' };
    s = `${code.startsWith('+') ? code : `+${code}`}${s.replace(/^0+/, '')}`;
  }
  const digits = s.slice(1);
  if (!/^\d+$/.test(digits)) return { ok: false, reason: 'Contains letters or symbols' };
  if (digits.length < 7) return { ok: false, reason: 'Too short' };
  if (digits.length > 15) return { ok: false, reason: 'Too long' };
  return { ok: true, value: s };
}

/** The column most likely to hold phone numbers: by header name, then by content. */
export function guessPhoneColumn(table: CsvTable): number {
  const byName = table.headers.findIndex((h) =>
    /^(phone|mobile|cell|number|msisdn|to|tel|telephone|phone_?number|mobile_?number|contact)$/i.test(
      h.trim().replace(/\s+/g, '_'),
    ),
  );
  if (byName >= 0) return byName;
  const loose = table.headers.findIndex((h) => /phone|mobile|msisdn|whatsapp|number/i.test(h));
  if (loose >= 0) return loose;
  const sample = table.rows.slice(0, 50);
  let best = 0;
  let bestScore = -1;
  table.headers.forEach((_, i) => {
    const score = sample.filter((r) => /^\+?[\d\s\-().]{7,20}$/.test(r[i] ?? '')).length;
    if (score > bestScore) {
      best = i;
      bestScore = score;
    }
  });
  return best;
}
