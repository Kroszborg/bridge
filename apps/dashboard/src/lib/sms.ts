// Mirrors apps/api/internal/message/sms.go, so the playground shows the same
// encoding and segment count Bridge will record.

const GSM7_BASIC = new Set(
  '@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !"#¤%&\'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà',
);
// Extension characters cost two septets (escape + character).
const GSM7_EXTENSION = new Set('\f^{}\\[~]|€');

export type SegmentInfo = {
  encoding: 'gsm7' | 'ucs2';
  /** Septets (GSM-7) or UTF-16 code units (UCS-2) used. */
  units: number;
  segments: number;
  /** Capacity of one segment at the current length. */
  perSegment: number;
  /** Characters that force UCS-2, for a hint. */
  unicodeChars: string[];
};

export function segmentInfo(body: string): SegmentInfo {
  let septets = 0;
  const unicode = new Set<string>();
  for (const ch of body) {
    if (GSM7_BASIC.has(ch)) septets += 1;
    else if (GSM7_EXTENSION.has(ch)) septets += 2;
    else unicode.add(ch);
  }
  if (unicode.size === 0) {
    const segments = septets <= 160 ? 1 : Math.ceil(septets / 153);
    return {
      encoding: 'gsm7',
      units: septets,
      segments,
      perSegment: segments > 1 ? 153 : 160,
      unicodeChars: [],
    };
  }
  const units = body.length; // UTF-16 code units: astral characters count twice, as on the wire
  const segments = units <= 70 ? 1 : Math.ceil(units / 67);
  return {
    encoding: 'ucs2',
    units,
    segments,
    perSegment: segments > 1 ? 67 : 70,
    unicodeChars: [...unicode].slice(0, 5),
  };
}
