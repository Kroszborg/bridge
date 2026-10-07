'use client';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider } from 'next-themes';
import { type ReactNode, useState } from 'react';
import { TooltipProvider } from '@/components/ui/tooltip';
import { BridgeApiError } from '@/lib/api';

export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            // Client errors (4xx) will not succeed on retry.
            retry: (count, err) =>
              !(err instanceof BridgeApiError && err.status >= 400 && err.status < 500) &&
              count < 2,
          },
        },
      }),
  );
  // Dark is the default. Light is fully designed, and "system" follows the OS.
  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="dark"
      enableSystem
      disableTransitionOnChange={false}
    >
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={200}>{children}</TooltipProvider>
      </QueryClientProvider>
    </ThemeProvider>
  );
}
