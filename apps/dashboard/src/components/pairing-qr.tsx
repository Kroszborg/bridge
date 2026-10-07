'use client';

import { QRCode } from '@kroszborg/rune-react';
import { BridgeMark } from '@/components/brand';

/**
 * Options shared with the scan test (scripts/verify-qr.mjs). Dark ink on white
 * keeps contrast high for phone cameras in both themes; the rounded modules
 * and finders carry the brand without hurting decoding.
 */
export const PAIRING_QR_STYLE = {
  margin: 2,
  background: '#ffffff',
  dots: { style: 'rounded', color: '#0c0b0a' },
  corners: {
    square: { style: 'extra-rounded', color: '#0b6b4f' },
    dot: { style: 'dot', color: '#0b6b4f' },
  },
  logo: { size: 0.2, margin: 1, shape: 'rounded', background: '#ffffff' },
} as const;

/** The QR code the Bridge app scans to pair a phone. */
export function PairingQR({ value, size = 184 }: { value: string; size?: number }) {
  return (
    <QRCode
      value={value}
      size={size}
      {...PAIRING_QR_STYLE}
      title="Bridge pairing code"
      ariaLabel="Pairing code for the Bridge Android app"
      logoElement={
        <div className="grid size-full place-items-center rounded-md bg-[#0b6b4f] text-white">
          <BridgeMark className="size-[70%]" />
        </div>
      }
    />
  );
}
