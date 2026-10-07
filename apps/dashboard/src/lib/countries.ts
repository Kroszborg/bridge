/**
 * Countries with phone numbers: ISO 3166-1 alpha-2 code and calling code.
 * Matches the regions the API accepts for allowed_countries. Names come from
 * Intl.DisplayNames, so they follow the reader's language.
 *
 * public/widget.js carries a copy of this table (it cannot import modules).
 */
const RAW =
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

export type Country = { code: string; dial: string; name: string };

let names: Intl.DisplayNames | null = null;
function displayName(code: string): string {
  try {
    names ??= new Intl.DisplayNames(['en'], { type: 'region' });
    return names.of(code) ?? code;
  } catch {
    return code;
  }
}

/** Every country, sorted by name. */
export const COUNTRIES: Country[] = RAW.split(' ')
  .map((s) => ({ code: s.slice(0, 2), dial: s.slice(2), name: displayName(s.slice(0, 2)) }))
  .sort((a, b) => a.name.localeCompare(b.name));

const BY_CODE = new Map(COUNTRIES.map((c) => [c.code, c]));

export function countryByCode(code: string | null | undefined): Country | undefined {
  return code ? BY_CODE.get(code.toUpperCase()) : undefined;
}

export function countryName(code: string | null | undefined): string {
  return countryByCode(code)?.name ?? code ?? '';
}

/** The country of the browser's locale (for example IN for en-IN or hi), else US. */
export function defaultCountry(): string {
  if (typeof navigator === 'undefined') return 'US';
  for (const tag of navigator.languages?.length ? navigator.languages : [navigator.language]) {
    try {
      const region = new Intl.Locale(tag).maximize().region;
      if (region && BY_CODE.has(region)) return region;
    } catch {
      // ignore malformed tags
    }
  }
  return 'US';
}

/**
 * Turns what the user typed into E.164, using the chosen country unless the
 * input starts with + or 00. Returns null when it cannot be a phone number.
 */
export function toE164(input: string, country: string): string | null {
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

function validE164(digits: string): string | null {
  return /^[1-9]\d{6,14}$/.test(digits) ? `+${digits}` : null;
}

/** Splits an E.164 number into a country and the national part, for prefilling. */
export function splitE164(
  e164: string,
  preferred: string,
): { country: string; national: string } | null {
  const digits = e164.replace(/\D/g, '');
  if (!e164.trim().startsWith('+') || digits.length < 7) return null;
  for (let len = 4; len >= 1; len--) {
    const dial = digits.slice(0, len);
    const matches = COUNTRIES.filter((c) => c.dial === dial);
    if (!matches.length) continue;
    const pick =
      matches.find((c) => c.code === preferred) ??
      matches.find((c) =>
        ['US', 'RU', 'GB', 'FI', 'NO', 'IT', 'MA', 'FR', 'AU'].includes(c.code),
      ) ??
      matches[0];
    if (pick) return { country: pick.code, national: digits.slice(len) };
  }
  return null;
}
