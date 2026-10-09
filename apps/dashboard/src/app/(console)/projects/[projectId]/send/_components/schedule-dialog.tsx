'use client';

import type { Schedule, ScheduleKind, ScheduleTimingInput } from '@bridge/api-types';
import { type FormEvent, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Segmented } from '@/components/kit/segmented';
import { useCan } from '@/components/layout/console-context';
import {
  browserTimeZone,
  DeviceSelect,
  EnvironmentChoice,
  FieldNote,
  SegmentLine,
  TimeZoneSelect,
} from '@/components/messaging';
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
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { fieldErrors, showError } from '@/lib/errors';
import { type Environment, useCreateSchedule, useUpdateSchedule } from '@/lib/queries';
import { cn } from '@/lib/utils';

type Weekday = NonNullable<ScheduleTimingInput['days']>[number];

export const WEEKDAYS: { value: Weekday; label: string }[] = [
  { value: 'mon', label: 'Mon' },
  { value: 'tue', label: 'Tue' },
  { value: 'wed', label: 'Wed' },
  { value: 'thu', label: 'Thu' },
  { value: 'fri', label: 'Fri' },
  { value: 'sat', label: 'Sat' },
  { value: 'sun', label: 'Sun' },
];

const KINDS: { value: ScheduleKind; label: string }[] = [
  { value: 'once', label: 'Once' },
  { value: 'daily', label: 'Daily' },
  { value: 'weekly', label: 'Weekly' },
  { value: 'monthly', label: 'Monthly' },
];

const pad = (n: number) => String(n).padStart(2, '0');

function localDate(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

type Form = {
  environment: Environment;
  name: string;
  to: string;
  message: string;
  deviceId: string;
  kind: ScheduleKind;
  date: string;
  days: Weekday[];
  dayOfMonth: string;
  at: string;
  timeZone: string;
  ends: boolean;
  endsOn: string;
};

function initial(s: Schedule | null, environment: Environment): Form {
  const tomorrow = new Date(Date.now() + 24 * 3600 * 1000);
  if (!s) {
    return {
      environment,
      name: '',
      to: environment === 'test' ? '+15550000001' : '',
      message: '',
      deviceId: 'auto',
      kind: 'daily',
      date: localDate(tomorrow),
      days: ['mon'],
      dayOfMonth: '1',
      at: '09:00',
      timeZone: browserTimeZone(),
      ends: false,
      endsOn: '',
    };
  }
  return {
    environment: s.environment,
    name: s.name,
    to: s.to,
    message: s.message,
    deviceId: s.device_id ?? 'auto',
    kind: s.schedule.kind,
    date: s.schedule.date ?? localDate(tomorrow),
    days: s.schedule.days.length ? s.schedule.days : ['mon'],
    dayOfMonth: String(s.schedule.day_of_month ?? 1),
    at: s.schedule.at,
    timeZone: s.schedule.time_zone,
    ends: s.ends_at !== null,
    endsOn: s.ends_at ? localDate(new Date(s.ends_at)) : '',
  };
}

/** Create a scheduled message, or edit one when `schedule` is set. */
export function ScheduleDialog({
  projectId,
  open,
  schedule,
  environment,
  onOpenChange,
  onSaved,
}: {
  projectId: string;
  open: boolean;
  schedule: Schedule | null;
  environment: Environment;
  onOpenChange: (open: boolean) => void;
  onSaved: (s: Schedule) => void;
}) {
  const canAdmin = useCan('admin');
  const create = useCreateSchedule(projectId);
  const update = useUpdateSchedule(projectId);
  const [f, setF] = useState<Form>(() => initial(schedule, environment));
  const [errors, setErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    if (open) {
      setF(initial(schedule, environment));
      setErrors({});
    }
  }, [open, schedule, environment]);

  const set = <K extends keyof Form>(key: K, value: Form[K]) => {
    setF((prev) => ({ ...prev, [key]: value }));
    setErrors({});
  };
  const editing = schedule !== null;
  const pending = create.isPending || update.isPending;

  function timing(): ScheduleTimingInput {
    const t: ScheduleTimingInput = { kind: f.kind, at: f.at, time_zone: f.timeZone };
    if (f.kind === 'once') t.date = f.date;
    if (f.kind === 'weekly')
      t.days = WEEKDAYS.map((d) => d.value).filter((d) => f.days.includes(d));
    if (f.kind === 'monthly') t.day_of_month = Number(f.dayOfMonth);
    return t;
  }

  function endsAt(): string | undefined {
    if (!f.ends || f.kind === 'once' || !f.endsOn) return undefined;
    // The end of that day in the browser's time zone.
    const d = new Date(`${f.endsOn}T23:59:59`);
    return Number.isNaN(d.getTime()) ? undefined : d.toISOString();
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (f.kind === 'weekly' && f.days.length === 0) {
      setErrors({ 'schedule.days': 'Choose at least one day.' });
      return;
    }
    if (f.ends && f.kind !== 'once' && !f.endsOn) {
      setErrors({ ends_at: 'Choose the last day, or turn this off.' });
      return;
    }
    try {
      let saved: Schedule;
      if (schedule) {
        saved = await update.mutateAsync({
          scheduleId: schedule.id,
          body: {
            name: f.name.trim(),
            to: f.to.trim(),
            message: f.message,
            device_id: f.environment === 'live' && f.deviceId !== 'auto' ? f.deviceId : '',
            schedule: timing(),
            ends_at: endsAt() ?? '',
          },
        });
        toast.success('Schedule saved');
      } else {
        saved = await create.mutateAsync({
          environment: f.environment,
          body: {
            name: f.name.trim() || undefined,
            to: f.to.trim(),
            message: f.message,
            device_id: f.environment === 'live' && f.deviceId !== 'auto' ? f.deviceId : undefined,
            schedule: timing(),
            ends_at: endsAt(),
          },
        });
        toast.success('Message scheduled');
      }
      onSaved(saved);
    } catch (err) {
      const fe = fieldErrors(err);
      if (Object.keys(fe).length) setErrors(fe);
      else showError(err);
    }
  }

  const timingError =
    errors['schedule.at'] ??
    errors['schedule.time_zone'] ??
    errors['schedule.kind'] ??
    errors.schedule;

  return (
    <Dialog open={open} onOpenChange={(o) => !pending && onOpenChange(o)}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-xl">
        <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
          <DialogHeader>
            <DialogTitle>{editing ? 'Edit schedule' : 'Schedule a message'}</DialogTitle>
            <DialogDescription>
              Bridge sends the message at the chosen time, once or on repeat, through the normal
              routing. Numbers that opted out are skipped.
            </DialogDescription>
          </DialogHeader>

          {editing ? (
            <p className="text-xs text-muted-foreground">
              Environment: {f.environment === 'live' ? 'Live' : 'Test'}. To change it, create a new
              schedule.
            </p>
          ) : (
            <EnvironmentChoice
              name="schedule-environment"
              value={f.environment}
              onChange={(v) => {
                setF((prev) => ({
                  ...prev,
                  environment: v,
                  to:
                    v === 'live' && prev.to.startsWith('+1555000000')
                      ? ''
                      : v === 'test' && !prev.to
                        ? '+15550000001'
                        : prev.to,
                }));
              }}
              canLive={canAdmin}
            />
          )}

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="schedule-to">To</Label>
              <Input
                id="schedule-to"
                value={f.to}
                onChange={(e) => set('to', e.target.value)}
                placeholder="+919876543210"
                className="font-mono"
                required
                aria-invalid={errors.to ? true : undefined}
              />
              <FieldNote error={errors.to}>With the country code.</FieldNote>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="schedule-name">
                Name <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="schedule-name"
                value={f.name}
                onChange={(e) => set('name', e.target.value)}
                placeholder="Rent reminder"
                maxLength={100}
              />
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="schedule-message">Message</Label>
            <Textarea
              id="schedule-message"
              value={f.message}
              onChange={(e) => set('message', e.target.value)}
              maxLength={1600}
              rows={4}
              required
              aria-invalid={errors.message ? true : undefined}
            />
            {errors.message ? (
              <FieldNote error={errors.message} />
            ) : f.message ? (
              <SegmentLine text={f.message} />
            ) : null}
          </div>

          <fieldset className="flex flex-col gap-3">
            <legend className="mb-2 text-sm font-medium">Repeat</legend>
            <div>
              <Segmented
                label="Repeat"
                value={f.kind}
                onChange={(v) => set('kind', v)}
                options={KINDS}
              />
            </div>

            {f.kind === 'once' ? (
              <div className="flex flex-col gap-2">
                <Label htmlFor="schedule-date">Date</Label>
                <Input
                  id="schedule-date"
                  type="date"
                  value={f.date}
                  onChange={(e) => set('date', e.target.value)}
                  required
                  className="sm:w-48"
                  aria-invalid={errors['schedule.date'] ? true : undefined}
                />
                <FieldNote error={errors['schedule.date']} />
              </div>
            ) : null}

            {f.kind === 'weekly' ? (
              <div className="flex flex-col gap-2">
                <span className="text-xs font-medium">On</span>
                <div className="flex flex-wrap gap-1.5">
                  {WEEKDAYS.map((d) => {
                    const on = f.days.includes(d.value);
                    return (
                      <button
                        key={d.value}
                        type="button"
                        aria-pressed={on}
                        onClick={() =>
                          set(
                            'days',
                            on ? f.days.filter((x) => x !== d.value) : [...f.days, d.value],
                          )
                        }
                        className={cn(
                          'w-11 rounded-md border py-1 text-xs font-medium transition-colors',
                          on
                            ? 'border-primary bg-primary/10 text-foreground'
                            : 'bg-background text-muted-foreground hover:bg-muted',
                        )}
                      >
                        {d.label}
                      </button>
                    );
                  })}
                </div>
                <FieldNote error={errors['schedule.days']} />
              </div>
            ) : null}

            {f.kind === 'monthly' ? (
              <div className="flex flex-col gap-2">
                <Label htmlFor="schedule-dom">Day of the month</Label>
                <Select value={f.dayOfMonth} onValueChange={(v) => set('dayOfMonth', v)}>
                  <SelectTrigger id="schedule-dom" className="w-28">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent className="max-h-64">
                    {Array.from({ length: 31 }, (_, i) => String(i + 1)).map((d) => (
                      <SelectItem key={d} value={d}>
                        {d}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FieldNote error={errors['schedule.day_of_month']}>
                  {Number(f.dayOfMonth) > 28
                    ? 'Months without this day use their last day.'
                    : undefined}
                </FieldNote>
              </div>
            ) : null}

            <div className="grid gap-3 sm:grid-cols-[8rem_minmax(0,1fr)]">
              <div className="flex flex-col gap-2">
                <Label htmlFor="schedule-at">Time</Label>
                <Input
                  id="schedule-at"
                  type="time"
                  value={f.at}
                  onChange={(e) => set('at', e.target.value)}
                  required
                />
              </div>
              <div className="flex min-w-0 flex-col gap-2">
                <Label htmlFor="schedule-tz">Time zone</Label>
                <TimeZoneSelect
                  id="schedule-tz"
                  value={f.timeZone}
                  onChange={(v) => set('timeZone', v)}
                />
              </div>
            </div>
            <FieldNote error={timingError}>Daylight saving changes are followed.</FieldNote>
          </fieldset>

          {f.kind !== 'once' ? (
            <div className="flex flex-col gap-3">
              <div className="flex items-center gap-2.5">
                <Switch
                  id="schedule-ends-toggle"
                  checked={f.ends}
                  onCheckedChange={(v) => set('ends', v)}
                />
                <Label htmlFor="schedule-ends-toggle" className="text-sm font-normal">
                  Stop after a date
                </Label>
              </div>
              {f.ends ? (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="schedule-ends">Last day</Label>
                  <Input
                    id="schedule-ends"
                    type="date"
                    value={f.endsOn}
                    onChange={(e) => set('endsOn', e.target.value)}
                    className="sm:w-48"
                  />
                  <FieldNote error={errors.ends_at}>
                    No runs after the end of this day in your time zone.
                  </FieldNote>
                </div>
              ) : null}
            </div>
          ) : null}

          <div className="flex flex-col gap-2">
            <Label htmlFor="schedule-device">Phone</Label>
            <DeviceSelect
              id="schedule-device"
              projectId={projectId}
              value={f.deviceId}
              onChange={(v) => set('deviceId', v)}
              disabled={f.environment === 'test'}
            />
            <FieldNote error={errors.device_id}>
              {f.environment === 'test'
                ? 'Test messages go to the simulator.'
                : 'A chosen phone must be online at send time, or the message waits for it.'}
            </FieldNote>
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending || !f.to.trim() || !f.message.trim() || !f.at}>
              {pending ? 'Saving…' : editing ? 'Save changes' : 'Schedule message'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
