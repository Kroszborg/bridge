'use client';

import { Logout03Icon, MoreVerticalIcon, UserCircleIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { api } from '@/lib/api';
import { useConsole } from './console-context';

function initials(name: string, email: string) {
  const source = name.trim() || email.split('@')[0] || '?';
  const parts = source.split(/[\s._-]+/).filter(Boolean);
  return ((parts[0]?.[0] ?? '?') + (parts[1]?.[0] ?? '')).toUpperCase();
}

export function useSignOut() {
  const router = useRouter();
  return async () => {
    await api.POST('/v1/auth/logout').catch(() => undefined);
    router.replace('/login');
    router.refresh();
  };
}

export function UserMenu({ compact = false }: { compact?: boolean }) {
  const { user } = useConsole();
  const signOut = useSignOut();
  const display = user.name || user.email.split('@')[0];

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label="Account menu"
        className={
          compact
            ? 'flex items-center rounded-full transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
            : 'flex w-full items-center gap-3 rounded-lg p-2 text-left transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
        }
      >
        <Avatar>
          <AvatarFallback className="bg-primary/12 text-xs font-semibold text-primary">
            {initials(user.name, user.email)}
          </AvatarFallback>
        </Avatar>
        {!compact ? (
          <>
            <div className="min-w-0 flex-1 leading-tight">
              <div className="truncate text-sm font-medium">{display}</div>
              <div className="truncate text-xs text-muted-foreground">{user.email}</div>
            </div>
            <HugeiconsIcon
              icon={MoreVerticalIcon}
              strokeWidth={2}
              className="size-4 text-muted-foreground"
            />
          </>
        ) : null}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side={compact ? 'bottom' : 'top'} className="w-56">
        <DropdownMenuLabel className="truncate font-normal text-muted-foreground">
          {user.email}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link href="/account">
            <HugeiconsIcon icon={UserCircleIcon} strokeWidth={2} className="size-4" />
            Account
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem variant="destructive" onSelect={() => void signOut()}>
          <HugeiconsIcon icon={Logout03Icon} strokeWidth={2} className="size-4" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
