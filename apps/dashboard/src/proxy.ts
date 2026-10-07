import { type NextRequest, NextResponse } from 'next/server';

const PUBLIC = new Set(['/login', '/signup', '/status']);

/** Fast path: send visitors without a session cookie to sign in. The API remains the authority. */
export function proxy(req: NextRequest) {
  const { pathname, search } = req.nextUrl;
  if (
    PUBLIC.has(pathname) ||
    pathname.startsWith('/invite/') ||
    req.cookies.has('bridge_session')
  ) {
    return NextResponse.next();
  }
  const url = req.nextUrl.clone();
  url.pathname = '/login';
  url.search = pathname === '/' ? '' : `?next=${encodeURIComponent(pathname + search)}`;
  return NextResponse.redirect(url);
}

export const config = {
  matcher: ['/((?!api/|_next/|favicon.ico|.*\\.[a-z0-9]+$).*)'],
};
