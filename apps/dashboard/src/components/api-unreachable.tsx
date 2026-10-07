import { Wordmark } from '@/components/brand';
import { CodeBlock } from '@/components/kit/code-block';
import { API_URL } from '@/lib/server-api';

/** Shown when the dashboard server cannot reach the Bridge API. */
export function ApiUnreachable() {
  return (
    <div className="flex flex-1 items-center justify-center p-6">
      <div className="flex w-full max-w-lg flex-col gap-5">
        <Wordmark />
        <div>
          <h1 className="font-display text-xl font-semibold">
            The dashboard cannot reach the Bridge API
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            It tried <code className="font-mono text-foreground">{API_URL}</code> and got no answer.
            Start the API, or point{' '}
            <code className="font-mono text-foreground">BRIDGE_API_URL</code> at the right address,
            then reload this page.
          </p>
        </div>
        <CodeBlock
          language="shell"
          code={'docker compose up -d\n# or, from apps/api:\ngo run ./cmd/bridge serve --worker'}
        />
      </div>
    </div>
  );
}
