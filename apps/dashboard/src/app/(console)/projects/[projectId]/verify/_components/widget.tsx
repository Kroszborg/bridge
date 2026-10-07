'use client';

import type { VerifyApp } from '@bridge/api-types';
import {
  Cancel01Icon,
  PlusSignIcon,
  RefreshIcon,
  ViewIcon,
  ViewOffSlashIcon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, type KeyboardEvent, useState } from 'react';
import { toast } from 'sonner';
import { CodeBlock } from '@/components/kit/code-block';
import { CopyButton, CopyField } from '@/components/kit/copy-button';
import { SectionCard } from '@/components/kit/section-card';
import { Segmented } from '@/components/kit/segmented';
import { useCan, useConsole } from '@/components/layout/console-context';
import { SecretKeyWarning } from '@/components/secret-key-warning';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { fieldErrors, showError } from '@/lib/errors';
import { useRouting, useUpdateVerifyApp, useVerifyAppSecret } from '@/lib/queries';
import { FieldNote, useDashboardOrigin } from './shared';

const MAX_ENTRIES = 20;

function isLocalHost(host: string): boolean {
  const h = host.toLowerCase().replace(/^\[|\]$/g, '');
  return h === 'localhost' || h === '127.0.0.1' || h === '::1' || h.endsWith('.localhost');
}

type Checked = { value: string } | { error: string };

/** Mirrors the API: https origins, http only for localhost, no path, query or fragment. */
function checkOrigin(raw: string): Checked {
  const bad = {
    error:
      'Use an origin such as https://shop.example.com, without a path (http only for localhost).',
  };
  let u: URL;
  try {
    u = new URL(raw.trim());
  } catch {
    return bad;
  }
  const trailing = raw.trim().replace(/^[a-z]+:\/\/[^/]+/i, '');
  if (u.username || u.password || u.search || u.hash || (trailing !== '' && trailing !== '/')) {
    return bad;
  }
  if (u.protocol !== 'https:' && !(u.protocol === 'http:' && isLocalHost(u.hostname))) return bad;
  return { value: u.origin };
}

/** Mirrors the API: absolute https URL (http only for localhost), no fragment. */
function checkRedirect(raw: string): Checked {
  const v = raw.trim();
  const bad = {
    error: 'Use an absolute https:// URL without a #fragment (http only for localhost).',
  };
  if (!v || v.length > 2048 || v.includes('#')) return bad;
  let u: URL;
  try {
    u = new URL(v);
  } catch {
    return bad;
  }
  if (u.username || u.password) return bad;
  if (u.protocol !== 'https:' && !(u.protocol === 'http:' && isLocalHost(u.hostname))) return bad;
  return { value: v };
}

function ListEditor({
  id,
  label,
  items,
  onChange,
  check,
  placeholder,
  note,
  error,
  disabled,
}: {
  id: string;
  label: string;
  items: string[];
  onChange: (items: string[]) => void;
  check: (raw: string) => Checked;
  placeholder: string;
  note: React.ReactNode;
  error?: string;
  disabled: boolean;
}) {
  const [draft, setDraft] = useState('');
  const [draftError, setDraftError] = useState('');

  function add() {
    if (!draft.trim()) return;
    const r = check(draft);
    if ('error' in r) return setDraftError(r.error);
    if (items.includes(r.value)) return setDraftError('Already in the list.');
    if (items.length >= MAX_ENTRIES) return setDraftError(`Use at most ${MAX_ENTRIES} entries.`);
    onChange([...items, r.value]);
    setDraft('');
    setDraftError('');
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault();
      add();
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {items.length ? (
        <ul className="flex flex-col divide-y rounded-lg border bg-background">
          {items.map((item) => (
            <li key={item} className="flex min-w-0 items-center gap-2 py-1 pr-1 pl-3">
              <code className="min-w-0 flex-1 truncate font-mono text-xs" title={item}>
                {item}
              </code>
              {disabled ? null : (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => onChange(items.filter((x) => x !== item))}
                  aria-label={`Remove ${item}`}
                  className="text-muted-foreground hover:text-destructive"
                >
                  <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} />
                </Button>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">
          None yet.
        </p>
      )}
      {disabled ? null : (
        <div className="flex gap-2">
          <Input
            id={id}
            value={draft}
            onChange={(e) => {
              setDraft(e.target.value);
              setDraftError('');
            }}
            onKeyDown={onKeyDown}
            placeholder={placeholder}
            spellCheck={false}
            autoComplete="off"
            className="font-mono"
            aria-invalid={draftError ? true : undefined}
            aria-describedby={`${id}-note`}
          />
          <Button type="button" variant="outline" onClick={add} disabled={!draft.trim()}>
            <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} />
            Add
          </Button>
        </div>
      )}
      <FieldNote id={`${id}-note`} error={draftError || error}>
        {note}
      </FieldNote>
    </div>
  );
}

export function WidgetCard({ projectId, app }: { projectId: string; app: VerifyApp }) {
  const canAdmin = useCan('admin');
  const routing = useRouting(projectId);
  const secretKeySet = routing.data?.secret_key_set ?? true;
  const update = useUpdateVerifyApp(projectId, app.id);
  const [environment, setEnvironment] = useState(app.widget_environment);
  const [origins, setOrigins] = useState(app.allowed_origins);
  const [redirects, setRedirects] = useState(app.redirect_uris);
  const [siteKey, setSiteKey] = useState(app.turnstile_site_key ?? '');
  const [turnstileSecret, setTurnstileSecret] = useState('');
  const [errors, setErrors] = useState<Record<string, string>>({});

  const secretStored = app.turnstile_secret_set;
  const removingTurnstile = siteKey.trim() === '' && (app.turnstile_site_key ?? '') !== '';
  const turnstileError =
    siteKey.trim() && !secretStored && !turnstileSecret.trim()
      ? 'Add the secret key from the same Turnstile widget.'
      : '';
  const unchanged =
    environment === app.widget_environment &&
    origins.join('\n') === app.allowed_origins.join('\n') &&
    redirects.join('\n') === app.redirect_uris.join('\n') &&
    siteKey.trim() === (app.turnstile_site_key ?? '') &&
    turnstileSecret.trim() === '';

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (turnstileError) return;
    setErrors({});
    try {
      await update.mutateAsync({
        widget_environment: environment,
        allowed_origins: origins,
        redirect_uris: redirects,
        turnstile_site_key: siteKey.trim(),
        turnstile_secret: removingTurnstile ? '' : turnstileSecret.trim() || undefined,
      });
      setTurnstileSecret('');
      toast.success('Widget settings saved');
    } catch (err) {
      const fe = fieldErrors(err);
      setErrors(fe);
      const known = ['allowed_origins', 'redirect_uris', 'turnstile_site_key', 'turnstile_secret'];
      if (!known.some((k) => Object.keys(fe).some((f) => f === k || f.startsWith(`${k}[`)))) {
        showError(err);
      }
    }
  }

  const listError = (key: string) =>
    Object.entries(errors).find(([k]) => k === key || k.startsWith(`${key}[`))?.[1];

  return (
    <SectionCard
      title="Drop-in widget"
      description="A ready-made phone check for your site: a button and dialog you embed, or a page Bridge hosts. Your server receives a signed token, never a code."
    >
      <div className="grid gap-8 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
          <div className="flex min-w-0 flex-col gap-2">
            <span className="text-sm font-medium">Publishable key</span>
            <CopyField value={app.publishable_key} />
            <p className="text-xs/relaxed text-muted-foreground">
              Identifies this app to the widget. It is safe to put in your pages.
            </p>
          </div>

          <div className="flex flex-col gap-2">
            <span className="text-sm font-medium">Widget environment</span>
            <div className={canAdmin ? undefined : 'pointer-events-none opacity-60'}>
              <Segmented
                label="Widget environment"
                value={environment}
                onChange={setEnvironment}
                options={[
                  { value: 'test', label: 'Test' },
                  { value: 'live', label: 'Live' },
                ]}
              />
            </div>
            <p className="text-xs/relaxed text-muted-foreground">
              {environment === 'test'
                ? 'Test sends nothing and shows the code in the widget, so you can build without a phone. Switch to Live before launch.'
                : 'Live sends real SMS through your phones and providers, with this app’s fraud protection.'}
            </p>
          </div>

          <ListEditor
            id="widget-origins"
            label="Allowed origins"
            items={origins}
            onChange={setOrigins}
            check={checkOrigin}
            placeholder="https://shop.example.com"
            note="Sites whose pages may embed the widget. The hosted page on this dashboard is always allowed."
            error={listError('allowed_origins')}
            disabled={!canAdmin}
          />

          <ListEditor
            id="widget-redirects"
            label="Redirect URIs"
            items={redirects}
            onChange={setRedirects}
            check={checkRedirect}
            placeholder="https://shop.example.com/verified"
            note="Where the hosted page may send users back to. Matched exactly, including the query string."
            error={listError('redirect_uris')}
            disabled={!canAdmin}
          />

          <fieldset disabled={!canAdmin} className="flex flex-col gap-3">
            <legend className="mb-1 text-sm font-medium">
              Cloudflare Turnstile{' '}
              <span className="font-normal text-muted-foreground">(optional)</span>
            </legend>
            <p className="-mt-1 text-xs/relaxed text-muted-foreground">
              Adds a bot check before each code is sent. Create a widget in the Cloudflare dashboard
              with your sites&apos; hostnames and paste its keys here.
            </p>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-2">
                <Label htmlFor="turnstile-site">Site key</Label>
                <Input
                  id="turnstile-site"
                  value={siteKey}
                  onChange={(e) => setSiteKey(e.target.value)}
                  placeholder="0x4AAAAAAA…"
                  spellCheck={false}
                  autoComplete="off"
                  className="font-mono"
                  aria-invalid={errors.turnstile_site_key ? true : undefined}
                />
                <FieldNote error={errors.turnstile_site_key} />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="turnstile-secret">Secret key</Label>
                <Input
                  id="turnstile-secret"
                  type="password"
                  value={turnstileSecret}
                  onChange={(e) => setTurnstileSecret(e.target.value)}
                  placeholder={secretStored ? 'Stored. Leave empty to keep it.' : ''}
                  autoComplete="new-password"
                  spellCheck={false}
                  className="font-mono"
                  disabled={!siteKey.trim() || !secretKeySet}
                  aria-invalid={turnstileError || errors.turnstile_secret ? true : undefined}
                />
                <FieldNote error={turnstileError || errors.turnstile_secret}>
                  {secretKeySet ? 'Encrypted and never shown again.' : 'Needs BRIDGE_SECRET_KEY.'}
                </FieldNote>
              </div>
            </div>
            {app.turnstile_site_key && canAdmin ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="self-start text-muted-foreground hover:text-destructive"
                onClick={() => {
                  setSiteKey('');
                  setTurnstileSecret('');
                }}
              >
                Remove Turnstile
              </Button>
            ) : null}
          </fieldset>

          {canAdmin ? (
            <Button
              type="submit"
              className="self-start"
              disabled={update.isPending || unchanged || turnstileError !== ''}
            >
              {update.isPending ? 'Saving…' : 'Save widget settings'}
            </Button>
          ) : null}
        </form>

        <div className="flex min-w-0 flex-col gap-6">
          {!secretKeySet ? (
            <SecretKeyWarning what="token signing secrets" />
          ) : canAdmin ? (
            <SigningSecret projectId={projectId} app={app} />
          ) : (
            <div className="flex flex-col gap-1">
              <span className="text-sm font-medium">Token signing secret</span>
              <p className="text-xs/relaxed text-muted-foreground">
                Only admins can reveal or rotate it.
              </p>
            </div>
          )}
          <Snippets app={app} />
        </div>
      </div>
    </SectionCard>
  );
}

function SigningSecret({ projectId, app }: { projectId: string; app: VerifyApp }) {
  const secret = useVerifyAppSecret(projectId, app.id);
  const [value, setValue] = useState<string | null>(null);
  const [confirmRotate, setConfirmRotate] = useState(false);

  async function reveal() {
    if (value) return setValue(null);
    try {
      setValue((await secret.mutateAsync('reveal')).secret);
    } catch (err) {
      showError(err);
    }
  }
  async function rotate() {
    try {
      setValue((await secret.mutateAsync('rotate')).secret);
      setConfirmRotate(false);
      toast.success('Secret rotated. Update it on your server now.');
    } catch (err) {
      showError(err);
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <span className="text-sm font-medium">Token signing secret</span>
      <div className="flex min-w-0 items-center gap-1 rounded-lg border bg-background py-1 pr-1 pl-3">
        <code className="min-w-0 flex-1 truncate font-mono text-xs">
          {value ?? (app.secret_set ? 'bvs_••••••••••••••••••••••••••••••••' : 'Not created yet')}
        </code>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={reveal}
          disabled={secret.isPending}
          aria-label={value ? 'Hide secret' : 'Reveal secret'}
          className="text-muted-foreground hover:text-foreground"
        >
          <HugeiconsIcon icon={value ? ViewOffSlashIcon : ViewIcon} strokeWidth={2} />
        </Button>
        {value ? <CopyButton value={value} label="Copy secret" /> : null}
      </div>
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs/relaxed text-muted-foreground">
          Your server checks tokens with it (HS256). Reveals are recorded in the audit log.
        </p>
        <Button variant="outline" size="sm" onClick={() => setConfirmRotate(true)}>
          <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
          Rotate
        </Button>
      </div>

      <Dialog open={confirmRotate} onOpenChange={setConfirmRotate}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rotate the signing secret?</DialogTitle>
            <DialogDescription>
              Tokens signed with the current secret stop verifying at once, including ones users
              just received. Update the secret on your server right after rotating.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmRotate(false)}>
              Cancel
            </Button>
            <Button onClick={rotate} disabled={secret.isPending}>
              {secret.isPending ? 'Rotating…' : 'Rotate secret'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Snippets({ app }: { app: VerifyApp }) {
  const { apiUrl } = useConsole();
  const origin = useDashboardOrigin() || 'https://bridge.example.com';
  const key = app.publishable_key;
  const redirect = app.redirect_uris[0] ?? 'https://shop.example.com/verified';

  const script = [
    `<script type="module" src="${origin}/widget.js"></script>`,
    ``,
    `<bridge-verify publishable-key="${key}"></bridge-verify>`,
    ``,
    `<script type="module">`,
    `  const el = document.querySelector('bridge-verify');`,
    `  el.addEventListener('bridge-verified', async (e) => {`,
    `    // e.detail: { token, phone, verificationId }`,
    `    await fetch('/api/verify-phone', {`,
    `      method: 'POST',`,
    `      headers: { 'Content-Type': 'application/json' },`,
    `      body: JSON.stringify({ token: e.detail.token }),`,
    `    });`,
    `  });`,
    `</script>`,
  ].join('\n');

  const hosted = [
    `# Send the user to the hosted page`,
    `${origin}/verify/${key}?redirect_uri=${encodeURIComponent(redirect)}&state=<random>`,
    ``,
    `# When the number is verified, Bridge sends them back with a token`,
    `${redirect}${redirect.includes('?') ? '&' : '?'}bridge_token=<token>&state=<random>`,
    ``,
    `# If they cancel`,
    `${redirect}${redirect.includes('?') ? '&' : '?'}error=cancelled&state=<random>`,
  ].join('\n');

  const server = [
    `// On your server: check the token before trusting the number.`,
    `const check = await bridge.otp.verifyToken(token);`,
    `if (!check.valid) throw new Error(\`Phone not verified: \${check.reason}\`);`,
    `const phone = check.phone; // E.164, for example +919876543210`,
    ``,
    `// Or check it yourself: an HS256 JWT signed with the app's secret.`,
    `import { jwtVerify } from 'jose';`,
    `const { payload } = await jwtVerify(`,
    `  token,`,
    `  new TextEncoder().encode(process.env.BRIDGE_VERIFY_SECRET),`,
    `  { issuer: '${apiUrl}', audience: '${app.id}' },`,
    `);`,
    `// payload.sub is the phone number, payload.vid the verification ID`,
  ].join('\n');

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <span className="text-sm font-medium">Add it to your site</span>
      <Tabs defaultValue="script" className="min-w-0">
        <TabsList>
          <TabsTrigger value="script">Script tag</TabsTrigger>
          <TabsTrigger value="hosted">Hosted page</TabsTrigger>
          <TabsTrigger value="server">Check the token</TabsTrigger>
        </TabsList>
        <TabsContent value="script" className="flex flex-col gap-2">
          <CodeBlock language="html" code={script} />
          <p className="text-xs/relaxed text-muted-foreground">
            Add your site&apos;s origin to Allowed origins first. Optional attributes:{' '}
            <code className="font-mono">phone</code> to prefill the number and{' '}
            <code className="font-mono">label</code> for the button text. Or open it from code with{' '}
            <code className="font-mono">BridgeVerify.open({'{ publishableKey }'})</code>.
          </p>
        </TabsContent>
        <TabsContent value="hosted" className="flex flex-col gap-2">
          <CodeBlock language="text" code={hosted} />
          <p className="text-xs/relaxed text-muted-foreground">
            The redirect URI must be in the list on the left. Pass a random{' '}
            <code className="font-mono">state</code> and compare it when the user returns.
          </p>
        </TabsContent>
        <TabsContent value="server">
          <CodeBlock language="typescript" code={server} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
