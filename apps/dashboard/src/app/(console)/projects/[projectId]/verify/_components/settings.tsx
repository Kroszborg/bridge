'use client';

import type { VerifyApp, VerifyAppUpdateInput } from '@bridge/api-types';
import { Cancel01Icon, Search01Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, useMemo, useState } from 'react';
import { toast } from 'sonner';
import { SectionCard } from '@/components/kit/section-card';
import { useCan } from '@/components/layout/console-context';
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { COUNTRIES, countryName } from '@/lib/countries';
import { fieldErrors, showError } from '@/lib/errors';
import { useUpdateVerifyApp } from '@/lib/queries';
import { segmentInfo } from '@/lib/sms';
import { cn } from '@/lib/utils';
import { digitsOnly, FieldNote } from './shared';

/** Saves part of an app, showing per-field errors from a 422. */
function useSave(projectId: string, app: VerifyApp, fields: string[]) {
  const update = useUpdateVerifyApp(projectId, app.id);
  const [errors, setErrors] = useState<Record<string, string>>({});
  async function save(body: VerifyAppUpdateInput, done: string) {
    setErrors({});
    try {
      await update.mutateAsync(body);
      toast.success(done);
      return true;
    } catch (err) {
      const fe = fieldErrors(err);
      setErrors(fe);
      if (!fields.some((f) => fe[f])) showError(err);
      return false;
    }
  }
  return { save, errors, pending: update.isPending, clear: () => setErrors({}) };
}

/** Mirrors the server's rendering so the preview matches the SMS exactly. */
function renderMessage(
  template: string,
  app: string,
  code: string,
  ttlSeconds: number,
  domain: string,
): string {
  const minutes = Math.ceil(ttlSeconds / 60);
  let body = template
    .replaceAll('{code}', code)
    .replaceAll('{app}', app)
    .replaceAll('{minutes}', String(minutes));
  if (domain) body += `\n\n@${domain} #${code}`;
  return body;
}

function options(values: number[], current: string, label: (v: number) => string) {
  return [...new Set([...values, Number(current)])]
    .sort((a, b) => a - b)
    .map((v) => (
      <SelectItem key={v} value={String(v)}>
        {label(v)}
      </SelectItem>
    ));
}

export function MessageCard({ projectId, app }: { projectId: string; app: VerifyApp }) {
  const canAdmin = useCan('admin');
  const { save, errors, pending } = useSave(projectId, app, [
    'app_name',
    'template',
    'web_otp_domain',
  ]);
  const [appName, setAppName] = useState(app.app_name ?? '');
  const [template, setTemplate] = useState(app.template ?? '');
  const [codeLength, setCodeLength] = useState(String(app.code_length));
  const [ttl, setTtl] = useState(String(app.ttl_seconds));
  const [attempts, setAttempts] = useState(String(app.max_attempts));
  const [domain, setDomain] = useState(app.web_otp_domain ?? '');

  const effectiveTemplate = template.trim() || app.default_template;
  const shownName = appName.trim() || app.effective_app_name;
  const preview = renderMessage(
    effectiveTemplate,
    shownName,
    '4829130123'.slice(0, Number(codeLength)),
    Number(ttl),
    domain.trim().toLowerCase(),
  );
  const seg = segmentInfo(preview);
  const codeCount = effectiveTemplate.split('{code}').length - 1;
  const unknown = [...effectiveTemplate.matchAll(/\{[^{}]*\}/g)]
    .map((m) => m[0])
    .filter((p) => !['{code}', '{app}', '{minutes}'].includes(p));
  const templateError =
    codeCount !== 1
      ? 'The message must contain {code} exactly once.'
      : unknown.length
        ? `Unknown placeholder ${unknown[0]}. Use {code}, {app} and {minutes}.`
        : '';

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (templateError) return;
    await save(
      {
        app_name: appName.trim(),
        template: template.trim(),
        code_length: Number(codeLength),
        ttl_seconds: Number(ttl),
        max_attempts: Number(attempts),
        web_otp_domain: domain.trim(),
      },
      'Message and limits saved',
    );
  }

  return (
    <SectionCard
      title="Message and limits"
      description={
        canAdmin ? `Applies to every code ${app.name} sends.` : 'Only admins can change these.'
      }
    >
      <form onSubmit={submit} className="flex flex-col gap-5">
        <fieldset disabled={!canAdmin} className="contents">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="otp-app">App name</Label>
              <Input
                id="otp-app"
                value={appName}
                onChange={(e) => setAppName(e.target.value)}
                placeholder={app.effective_app_name}
                maxLength={40}
                aria-invalid={errors.app_name ? true : undefined}
              />
              <FieldNote error={errors.app_name} />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="otp-domain">
                Browser autofill domain{' '}
                <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="otp-domain"
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                placeholder="example.com"
                className="font-mono"
                aria-invalid={errors.web_otp_domain ? true : undefined}
              />
              <FieldNote error={errors.web_otp_domain} />
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="otp-template">Message</Label>
            <Textarea
              id="otp-template"
              value={template}
              onChange={(e) => setTemplate(e.target.value)}
              placeholder={app.default_template}
              rows={3}
              maxLength={300}
              aria-invalid={templateError !== '' || errors.template ? true : undefined}
            />
            <p className="text-xs text-muted-foreground">
              Use <code className="font-mono">{'{code}'}</code>,{' '}
              <code className="font-mono">{'{app}'}</code> and{' '}
              <code className="font-mono">{'{minutes}'}</code>. Leave empty for the default.
            </p>
            <FieldNote error={templateError || errors.template} />
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="otp-length">Code</Label>
              <Select value={codeLength} onValueChange={setCodeLength}>
                <SelectTrigger id="otp-length" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {options([4, 6, 8], codeLength, (v) => `${v} digits`)}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="otp-ttl">Valid for</Label>
              <Select value={ttl} onValueChange={setTtl}>
                <SelectTrigger id="otp-ttl" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {options(
                    [120, 300, 600, 900, 1800, 3600],
                    ttl,
                    (v) => `${Math.round(v / 60)} min`,
                  )}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="otp-attempts">Attempts</Label>
              <Select value={attempts} onValueChange={setAttempts}>
                <SelectTrigger id="otp-attempts" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>{options([3, 5, 10], attempts, (v) => String(v))}</SelectContent>
              </Select>
            </div>
          </div>
        </fieldset>

        <div className="flex flex-col gap-2">
          <span className="text-sm font-medium">Preview</span>
          <div className="max-w-sm self-start whitespace-pre-wrap rounded-2xl rounded-bl-md bg-muted px-4 py-3 text-sm/relaxed">
            {preview}
          </div>
          <p className="text-xs text-muted-foreground">
            {seg.segments} SMS segment{seg.segments > 1 ? 's' : ''} ·{' '}
            {seg.encoding === 'gsm7' ? 'GSM-7' : 'Unicode'}. Passing an Android app hash adds one
            line of 12 characters.
          </p>
        </div>

        {canAdmin ? (
          <Button type="submit" className="self-start" disabled={pending || templateError !== ''}>
            {pending ? 'Saving…' : 'Save message and limits'}
          </Button>
        ) : null}
      </form>
    </SectionCard>
  );
}

const FAILOVER = [0, 15, 30, 60, 120, 300];

function failoverLabel(seconds: number): string {
  if (seconds === 0) return 'Off';
  if (seconds < 60) return `After ${seconds} s`;
  if (seconds % 60 === 0) return `After ${seconds / 60} min`;
  return `After ${Math.round(seconds / 6) / 10} min`;
}

export function FailoverCard({ projectId, app }: { projectId: string; app: VerifyApp }) {
  const canAdmin = useCan('admin');
  const { save, errors, pending } = useSave(projectId, app, ['failover_after_seconds']);
  const [wait, setWait] = useState(String(app.failover_after_seconds));
  const unchanged = Number(wait) === app.failover_after_seconds;

  async function submit(e: FormEvent) {
    e.preventDefault();
    await save(
      { failover_after_seconds: Number(wait) },
      Number(wait) === 0 ? 'Delivery failover turned off' : 'Delivery failover saved',
    );
  }

  return (
    <SectionCard
      title="Delivery failover"
      description={
        canAdmin
          ? 'Resends a code that did not go out, through another route.'
          : 'Resends a code that did not go out. Only admins can change it.'
      }
    >
      <form onSubmit={submit} className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="failover-wait">Resend when not sent</Label>
          <Select value={wait} onValueChange={setWait} disabled={!canAdmin}>
            <SelectTrigger
              id="failover-wait"
              className="w-48"
              aria-invalid={errors.failover_after_seconds ? true : undefined}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {[...new Set([...FAILOVER, Number(wait)])]
                .sort((a, b) => a - b)
                .map((v) => (
                  <SelectItem key={v} value={String(v)}>
                    {failoverLabel(v)}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
          <FieldNote error={errors.failover_after_seconds} />
        </div>
        <ul className="flex list-disc flex-col gap-1 pl-4 text-xs/relaxed text-muted-foreground">
          <li>
            If no phone has sent the SMS within this time, or the send failed for certain, Bridge
            sends the same code once more through your SMS providers or another online phone.
          </li>
          <li>
            It never resends after an ambiguous failure, where the first SMS may still arrive, so a
            user does not get two messages for one code.
          </li>
          <li>Only live codes fail over. Test codes are never sent.</li>
          <li>
            It needs another route: a second phone that is online, or an enabled SMS provider.
            Providers are used only when the project&apos;s routing on the Providers page allows
            them, so failover never spends money you did not opt into.
          </li>
        </ul>
        {canAdmin ? (
          <Button type="submit" className="self-start" disabled={pending || unchanged}>
            {pending ? 'Saving…' : 'Save failover'}
          </Button>
        ) : null}
      </form>
    </SectionCard>
  );
}

export function FraudCard({ projectId, app }: { projectId: string; app: VerifyApp }) {
  const canAdmin = useCan('admin');
  const { save, errors, pending } = useSave(projectId, app, [
    'allowed_countries',
    'ip_hourly_limit',
    'range_hourly_limit',
    'country_hourly_limit',
  ]);
  const [countries, setCountries] = useState<string[]>(app.allowed_countries);
  const [picking, setPicking] = useState(false);
  const [ip, setIp] = useState(String(app.ip_hourly_limit));
  const [range, setRange] = useState(String(app.range_hourly_limit));
  const [country, setCountry] = useState(
    app.country_hourly_limit ? String(app.country_hourly_limit) : '',
  );

  const sorted = [...countries].sort((a, b) => countryName(a).localeCompare(countryName(b)));
  const unchanged =
    [...countries].sort().join() === [...app.allowed_countries].sort().join() &&
    ip === String(app.ip_hourly_limit) &&
    range === String(app.range_hourly_limit) &&
    country === (app.country_hourly_limit ? String(app.country_hourly_limit) : '');

  async function submit(e: FormEvent) {
    e.preventDefault();
    await save(
      {
        allowed_countries: countries,
        ip_hourly_limit: Number(ip || 0),
        range_hourly_limit: Number(range || 0),
        country_hourly_limit: Number(country || 0),
      },
      'Fraud protection saved',
    );
  }

  return (
    <SectionCard
      title="Fraud protection"
      description={
        canAdmin
          ? 'Stops SMS pumping and abuse before a code is sent. Refusals are listed below.'
          : 'Stops SMS pumping and abuse before a code is sent. Only admins can change it.'
      }
    >
      <form onSubmit={submit} className="flex flex-col gap-5">
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between gap-3">
            <span id="countries-label" className="text-sm font-medium">
              Allowed countries
            </span>
            {canAdmin ? (
              <div className="flex items-center gap-1">
                {countries.length ? (
                  <Button type="button" variant="ghost" size="sm" onClick={() => setCountries([])}>
                    Allow all
                  </Button>
                ) : null}
                <Button type="button" variant="outline" size="sm" onClick={() => setPicking(true)}>
                  Choose countries
                </Button>
              </div>
            ) : null}
          </div>
          {sorted.length === 0 ? (
            <p className="rounded-lg border border-dashed px-3 py-2.5 text-xs text-muted-foreground">
              Every country. Limit this to where your users are: most SMS pumping targets expensive
              destinations you never serve.
            </p>
          ) : (
            <ul aria-labelledby="countries-label" className="flex flex-wrap gap-1.5">
              {sorted.map((c) => (
                <li
                  key={c}
                  className="inline-flex items-center gap-1 rounded-md border bg-background py-0.5 pr-0.5 pl-2 text-xs"
                >
                  {countryName(c)}
                  <span className="font-mono text-[0.68rem] text-muted-foreground">{c}</span>
                  {canAdmin ? (
                    <button
                      type="button"
                      onClick={() => setCountries((list) => list.filter((x) => x !== c))}
                      aria-label={`Remove ${countryName(c)}`}
                      className="grid size-5 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/30 focus-visible:outline-none"
                    >
                      <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} className="size-3" />
                    </button>
                  ) : (
                    <span className="w-1" />
                  )}
                </li>
              ))}
            </ul>
          )}
          <FieldNote error={errors.allowed_countries} />
        </div>

        <fieldset disabled={!canAdmin} className="grid gap-4 sm:grid-cols-3">
          <legend className="mb-2 text-sm font-medium">Hourly limits</legend>
          <LimitField
            id="limit-ip"
            label="Per IP address"
            value={ip}
            onChange={setIp}
            error={errors.ip_hourly_limit}
            note="Applies when your server passes client_ip, and always in the widget. 0 is off."
          />
          <LimitField
            id="limit-range"
            label="Per number range"
            value={range}
            onChange={setRange}
            error={errors.range_hourly_limit}
            note="Numbers that differ only in their last 3 digits. Catches sequential pumping. 0 is off."
          />
          <LimitField
            id="limit-country"
            label="Per country"
            value={country}
            onChange={setCountry}
            error={errors.country_hourly_limit}
            placeholder="No cap"
            note="Codes to any one country. Leave empty for no cap."
          />
        </fieldset>

        {canAdmin ? (
          <Button type="submit" className="self-start" disabled={pending || unchanged}>
            {pending ? 'Saving…' : 'Save fraud protection'}
          </Button>
        ) : null}
      </form>
      <CountryPicker
        open={picking}
        selected={countries}
        onClose={() => setPicking(false)}
        onDone={(list) => {
          setCountries(list);
          setPicking(false);
        }}
      />
    </SectionCard>
  );
}

function LimitField({
  id,
  label,
  value,
  onChange,
  error,
  note,
  placeholder,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  error?: string;
  note: string;
  placeholder?: string;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        inputMode="numeric"
        value={value}
        onChange={(e) => onChange(digitsOnly(e.target.value))}
        placeholder={placeholder}
        aria-invalid={error ? true : undefined}
        aria-describedby={`${id}-note`}
        className="font-mono"
      />
      <FieldNote id={`${id}-note`} error={error}>
        {note}
      </FieldNote>
    </div>
  );
}

function CountryPicker({
  open,
  selected,
  onClose,
  onDone,
}: {
  open: boolean;
  selected: string[];
  onClose: () => void;
  onDone: (codes: string[]) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex max-h-[85dvh] flex-col sm:max-w-md">
        {open ? <CountryPickerBody selected={selected} onClose={onClose} onDone={onDone} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function CountryPickerBody({
  selected,
  onClose,
  onDone,
}: {
  selected: string[];
  onClose: () => void;
  onDone: (codes: string[]) => void;
}) {
  const [query, setQuery] = useState('');
  const [chosen, setChosen] = useState(() => new Set(selected));
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return COUNTRIES;
    const plusDial = q.replace(/^\+/, '');
    return COUNTRIES.filter(
      (c) =>
        c.name.toLowerCase().includes(q) ||
        c.code.toLowerCase() === q ||
        (q.startsWith('+') && c.dial.startsWith(plusDial)),
    );
  }, [query]);

  function toggle(code: string) {
    setChosen((s) => {
      const next = new Set(s);
      if (next.has(code)) next.delete(code);
      else next.add(code);
      return next;
    });
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Allowed countries</DialogTitle>
        <DialogDescription>
          Codes go only to numbers in these countries. Choose none to allow every country.
        </DialogDescription>
      </DialogHeader>
      <div className="relative">
        <HugeiconsIcon
          icon={Search01Icon}
          strokeWidth={2}
          className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
        />
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search by name, code or +calling code"
          aria-label="Search countries"
          className="h-8 pl-8"
          autoFocus
        />
      </div>
      <fieldset className="scroll-slim -mx-2 min-h-40 flex-1 overflow-y-auto">
        <legend className="sr-only">Countries</legend>
        {visible.length === 0 ? (
          <p className="px-2 py-6 text-center text-xs text-muted-foreground">
            No country matches “{query}”.
          </p>
        ) : (
          visible.map((c) => (
            <label
              key={c.code}
              className={cn(
                'flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-xs hover:bg-muted has-focus-visible:ring-2 has-focus-visible:ring-ring/30',
                chosen.has(c.code) && 'text-foreground',
              )}
            >
              <input
                type="checkbox"
                checked={chosen.has(c.code)}
                onChange={() => toggle(c.code)}
                className="accent-primary"
              />
              <span className="min-w-0 flex-1 truncate">{c.name}</span>
              <span className="font-mono text-[0.68rem] text-muted-foreground">
                {c.code} · +{c.dial}
              </span>
            </label>
          ))
        )}
      </fieldset>
      <DialogFooter className="items-center sm:justify-between">
        <span className="text-xs text-muted-foreground">
          {chosen.size === 0 ? 'All countries allowed' : `${chosen.size} selected`}
        </span>
        <div className="flex gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="button" onClick={() => onDone([...chosen].sort())}>
            Use these countries
          </Button>
        </div>
      </DialogFooter>
    </>
  );
}
