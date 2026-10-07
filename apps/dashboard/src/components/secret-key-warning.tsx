import { Alert02Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { CodeBlock } from '@/components/kit/code-block';

/** Shown when the server has no BRIDGE_SECRET_KEY, so it cannot store credentials or secrets. */
export function SecretKeyWarning({ what }: { what: string }) {
  return (
    <div className="flex flex-col gap-3 rounded-xl border border-warning/40 bg-warning/8 p-4 text-xs/relaxed">
      <p className="flex items-center gap-1.5 font-semibold text-warning">
        <HugeiconsIcon icon={Alert02Icon} strokeWidth={2} className="size-3.5" />
        This server cannot store {what} yet
      </p>
      <p className="text-muted-foreground">
        Bridge encrypts credentials with <code className="font-mono">BRIDGE_SECRET_KEY</code>, and
        it is not set. Generate a key, add it to the API&apos;s environment (for example in{' '}
        <code className="font-mono">.env</code>), and restart Bridge. Keep the key safe: without it,
        stored credentials cannot be read.
      </p>
      <CodeBlock
        language="shell"
        code={'openssl rand -base64 32\n# then set BRIDGE_SECRET_KEY=<the output> and restart'}
      />
    </div>
  );
}
