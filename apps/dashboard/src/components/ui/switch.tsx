'use client';

import { Switch as SwitchPrimitive } from 'radix-ui';
import type * as React from 'react';
import { cn } from '@/lib/utils';

function Switch({ className, ...props }: React.ComponentProps<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      className={cn(
        'peer inline-flex h-4 w-7 shrink-0 items-center rounded-full border border-transparent transition-colors outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-primary data-[state=unchecked]:bg-input dark:data-[state=unchecked]:bg-input/80',
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className="pointer-events-none block size-3 rounded-full bg-background shadow-xs ring-0 transition-transform data-[state=checked]:translate-x-3.5 data-[state=unchecked]:translate-x-0.5 dark:data-[state=unchecked]:bg-foreground dark:data-[state=checked]:bg-primary-foreground"
      />
    </SwitchPrimitive.Root>
  );
}

export { Switch };
