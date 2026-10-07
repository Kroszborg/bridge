'use client';

import { QRCode } from '@kroszborg/rune-react';
import { BridgeMark } from './brand';

/** The QR style the console shows when pairing a phone, rendered with Rune. */
export function PairingQR({ size = 168 }: { size?: number }) {
  return (
    <QRCode
      value="bridge://pair?api=https%3A%2F%2Fapi.example.com&token=bp_example"
      size={size}
      margin={2}
      background="#ffffff"
      dots={{ style: 'rounded', color: '#0c0b0a' }}
      corners={{
        square: { style: 'extra-rounded', color: '#0b6b4f' },
        dot: { style: 'dot', color: '#0b6b4f' },
      }}
      logo={{ size: 0.2, margin: 1, shape: 'rounded', background: '#ffffff' }}
      ariaLabel="Example pairing code, as shown in the Bridge console"
      logoElement={
        <div className="grid size-full place-items-center rounded-md bg-[#0b6b4f] text-white">
          <BridgeMark className="size-[70%]" />
        </div>
      }
    />
  );
}
