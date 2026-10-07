import { cn } from '@/lib/utils';
import { CopyButton } from './copy-button';

/** A code sample with a language label and a copy button. */
export function CodeBlock({
  code,
  language,
  className,
}: {
  code: string;
  language: string;
  className?: string;
}) {
  return (
    <div className={cn('min-w-0 overflow-hidden rounded-lg border bg-background', className)}>
      <div className="flex items-center justify-between border-b py-1 pr-1 pl-3">
        <span className="font-mono text-[0.68rem] uppercase tracking-wider text-faint">
          {language}
        </span>
        <CopyButton value={code} label="Copy code" />
      </div>
      <pre className="scroll-slim overflow-x-auto p-3 font-mono text-xs/relaxed text-foreground">
        <code>{code}</code>
      </pre>
    </div>
  );
}
