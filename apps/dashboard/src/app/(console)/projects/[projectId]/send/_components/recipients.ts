import type { BroadcastRecipientInput } from '@bridge/api-types';
import { type CsvTable, normalisePhone, parseCsv, parseCsvRows, variableName } from '@/lib/csv';

/** The most recipients one broadcast may have (apps/api/internal/broadcast). */
export const MAX_RECIPIENTS = 10_000;
/** Most variables per recipient the API accepts. */
export const MAX_VARS = 20;
/** Longest variable value the API accepts. */
export const MAX_VAR_LENGTH = 500;

export type Variable = { name: string; header: string; index: number };
export type InvalidRow = { line: number; value: string; reason: string };

export type RecipientList = {
  /** Rows sent to the API, in file order. */
  recipients: BroadcastRecipientInput[];
  /** The file line of each recipient, for pointing at the API's "Row N" errors. */
  lines: number[];
  invalid: InvalidRow[];
  /** Data rows read, valid or not. */
  total: number;
};

/** Reads pasted or uploaded text. With header off, the columns are named phone, column_2, … */
export function readTable(text: string, header: boolean): CsvTable {
  const t = parseCsv(text);
  if (header) return t;
  const rows = parseCsvRows(text, t.delimiter).map((r) => r.map((v) => v.trim()));
  const width = Math.max(1, ...rows.map((r) => r.length));
  return {
    delimiter: t.delimiter,
    headers: Array.from({ length: width }, (_, i) => (i === 0 ? 'phone' : `column_${i + 1}`)),
    rows: rows.map((r) => {
      while (r.length < width) r.push('');
      return r;
    }),
  };
}

/** Whether the first row looks like data (a phone number) rather than column names. */
export function firstRowIsData(text: string): boolean {
  const t = parseCsv(text);
  return t.headers.some((h) => /^\+?[\d\s\-().]{7,20}$/.test(h) && normalisePhone(h, '+1').ok);
}

/** Every column but the phone number becomes a {placeholder}. */
export function variablesOf(table: CsvTable, phoneColumn: number): Variable[] {
  const used = new Set<string>();
  const out: Variable[] = [];
  table.headers.forEach((header, index) => {
    if (index === phoneColumn) return;
    const base = variableName(header);
    let name = base;
    for (let n = 2; used.has(name); n += 1) name = `${base}_${n}`;
    used.add(name);
    out.push({ name, header, index });
  });
  return out;
}

/** Validates numbers and collects the variables the template uses. */
export function buildRecipients(
  table: CsvTable,
  phoneColumn: number,
  variables: Variable[],
  used: string[],
  defaultCode: string,
  header: boolean,
): RecipientList {
  const wanted = variables.filter((v) => used.includes(v.name));
  const recipients: BroadcastRecipientInput[] = [];
  const lines: number[] = [];
  const invalid: InvalidRow[] = [];
  table.rows.forEach((row, i) => {
    // Lines as a spreadsheet numbers them: the header is line 1.
    const line = i + (header ? 2 : 1);
    const raw = row[phoneColumn] ?? '';
    const phone = normalisePhone(raw, defaultCode);
    if (!phone.ok) {
      invalid.push({ line, value: raw, reason: phone.reason });
      return;
    }
    const long = wanted.find((v) => (row[v.index] ?? '').length > MAX_VAR_LENGTH);
    if (long) {
      invalid.push({
        line,
        value: raw,
        reason: `${long.header} is longer than ${MAX_VAR_LENGTH} characters`,
      });
      return;
    }
    const vars: Record<string, string> = {};
    for (const v of wanted) vars[v.name] = row[v.index] ?? '';
    recipients.push(wanted.length ? { to: phone.value, vars } : { to: phone.value });
    lines.push(line);
  });
  return { recipients, lines, invalid, total: table.rows.length };
}

/** Turns the API's "recipients[3].vars" location into the file line it came from. */
export function lineForLocation(location: string, lines: number[]): number | null {
  const m = /^recipients\[(\d+)\]/.exec(location);
  if (!m) return null;
  return lines[Number(m[1])] ?? null;
}
