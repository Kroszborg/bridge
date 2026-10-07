import { createHmac } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { Bridge, BridgeTokenError, verifyWidgetToken } from '../src/index';

// Tokens are built here with Node's crypto module, independently of the SDK's WebCrypto code, the
// way Bridge's Go server builds them: HS256 under the app secret's UTF-8 bytes.
const secret = 'bvs_3kQ9mT7xR2vW8yZ1aB4cD6eF0gH5jK7nP9sU2wX4zA6bC8dE';
const appId = 'vap_01ja8z3k5wq2v7c9e4r2n0w6yb';
const issuer = 'https://api.sms.example.com';
const issuedAt = 1_791_192_000;
const now = new Date((issuedAt + 60) * 1000);

const b64url = (value: string | Buffer) => Buffer.from(value).toString('base64url');

function sign(
  claims: Record<string, unknown> = {},
  options: { header?: Record<string, unknown>; key?: string } = {},
) {
  const header = b64url(JSON.stringify(options.header ?? { alg: 'HS256', typ: 'JWT' }));
  const payload = b64url(
    JSON.stringify({
      iss: issuer,
      aud: appId,
      sub: '+919876543210',
      vid: 'otp_06ghatc07nghtrbq1yj7nwyjtm',
      env: 'live',
      iat: issuedAt,
      exp: issuedAt + 600,
      jti: '9f2c4e6a8b0d1f3e5a7c9e1b3d5f7a9c',
      ...claims,
    }),
  );
  const mac = createHmac('sha256', options.key ?? secret)
    .update(`${header}.${payload}`)
    .digest('base64url');
  return `${header}.${payload}.${mac}`;
}

const options = { secret, appId, issuer, now };

async function reasonOf(promise: Promise<unknown>): Promise<string> {
  const err = await promise.then(
    () => undefined,
    (e: unknown) => e,
  );
  expect(err).toBeInstanceOf(BridgeTokenError);
  return (err as BridgeTokenError).reason;
}

describe('verifyWidgetToken', () => {
  it('accepts a token signed independently and returns typed claims', async () => {
    const claims = await verifyWidgetToken(sign(), options);
    expect(claims).toEqual({
      phone: '+919876543210',
      verificationId: 'otp_06ghatc07nghtrbq1yj7nwyjtm',
      appId,
      environment: 'live',
      issuer,
      issuedAt: new Date(issuedAt * 1000),
      expiresAt: new Date((issuedAt + 600) * 1000),
      tokenId: '9f2c4e6a8b0d1f3e5a7c9e1b3d5f7a9c',
    });
  });

  it('ignores surrounding whitespace and a trailing slash on the issuer', async () => {
    const claims = await verifyWidgetToken(` ${sign()}\n`, { ...options, issuer: `${issuer}/` });
    expect(claims.phone).toBe('+919876543210');
  });

  it('skips the issuer check when no issuer is given', async () => {
    const claims = await verifyWidgetToken(sign({ iss: 'https://other.example.com' }), {
      secret,
      appId,
      now,
    });
    expect(claims.issuer).toBe('https://other.example.com');
  });

  it('rejects a tampered signature', async () => {
    const token = sign();
    const last = token.at(-2) === 'A' ? 'B' : 'A';
    const tampered = `${token.slice(0, -2)}${last}${token.at(-1)}`;
    expect(await reasonOf(verifyWidgetToken(tampered, options))).toBe('bad_signature');
  });

  it('rejects a tampered payload', async () => {
    const [header, , sig] = sign().split('.');
    const forged = b64url(
      JSON.stringify({ iss: issuer, aud: appId, sub: '+15550000001', env: 'live', exp: 2e9 }),
    );
    expect(await reasonOf(verifyWidgetToken(`${header}.${forged}.${sig}`, options))).toBe(
      'bad_signature',
    );
  });

  it('rejects a token signed with another secret', async () => {
    const token = sign({}, { key: 'bvs_another' });
    expect(await reasonOf(verifyWidgetToken(token, options))).toBe('bad_signature');
  });

  it('rejects a token for another app', async () => {
    const token = sign({ aud: 'vap_01ja8z3k5wq2v7c9e4r2n0w6zz' });
    expect(await reasonOf(verifyWidgetToken(token, options))).toBe('unknown_app');
  });

  it('rejects another issuer', async () => {
    const token = sign({ iss: 'https://evil.example.com' });
    expect(await reasonOf(verifyWidgetToken(token, options))).toBe('wrong_issuer');
  });

  it('rejects an expired token, within the clock tolerance', async () => {
    const token = sign();
    const at = (seconds: number) => new Date((issuedAt + seconds) * 1000);
    await expect(verifyWidgetToken(token, { ...options, now: at(625) })).resolves.toBeTruthy();
    expect(await reasonOf(verifyWidgetToken(token, { ...options, now: at(630) }))).toBe('expired');
    expect(
      await reasonOf(
        verifyWidgetToken(token, { ...options, now: at(601), clockToleranceSeconds: 0 }),
      ),
    ).toBe('expired');
  });

  it('rejects a token issued in the future', async () => {
    const token = sign({ iat: issuedAt + 3600, exp: issuedAt + 4200 });
    expect(await reasonOf(verifyWidgetToken(token, options))).toBe('expired');
  });

  it.each([
    ['none', { alg: 'none', typ: 'JWT' }],
    ['HS512', { alg: 'HS512', typ: 'JWT' }],
    ['RS256', { alg: 'RS256', typ: 'JWT' }],
    ['a missing alg', { typ: 'JWT' }],
  ])('rejects alg %s', async (_, header) => {
    const token = sign({}, { header });
    expect(await reasonOf(verifyWidgetToken(token, options))).toBe('malformed');
  });

  it('rejects an unsigned alg none token with an empty signature', async () => {
    const [, payload] = sign().split('.');
    const unsigned = `${b64url(JSON.stringify({ alg: 'none' }))}.${payload}.`;
    expect(await reasonOf(verifyWidgetToken(unsigned, options))).toBe('malformed');
  });

  it('requires the live environment by default', async () => {
    const test = sign({ env: 'test' });
    expect(await reasonOf(verifyWidgetToken(test, options))).toBe('environment_mismatch');
    const claims = await verifyWidgetToken(test, { ...options, environment: 'test' });
    expect(claims.environment).toBe('test');
    expect(await reasonOf(verifyWidgetToken(sign(), { ...options, environment: 'test' }))).toBe(
      'environment_mismatch',
    );
    expect(await reasonOf(verifyWidgetToken(sign({ env: undefined }), options))).toBe(
      'environment_mismatch',
    );
  });

  it.each([
    ['two parts', 'abc.def'],
    ['four parts', 'a.b.c.d'],
    ['non-base64url characters', 'a+b.c/d.e=f'],
    ['a header that is not JSON', `${b64url('nope')}.${b64url('{}')}.${b64url('x')}`],
    ['an empty string', ''],
  ])('rejects %s as malformed', async (_, token) => {
    expect(await reasonOf(verifyWidgetToken(token, options))).toBe('malformed');
  });

  it('rejects a validly signed token with missing claims as malformed', async () => {
    expect(await reasonOf(verifyWidgetToken(sign({ sub: undefined }), options))).toBe('malformed');
    expect(await reasonOf(verifyWidgetToken(sign({ exp: '2030' }), options))).toBe('malformed');
  });

  it('needs a secret and an app ID', async () => {
    await expect(verifyWidgetToken(sign(), { ...options, secret: '' })).rejects.toThrow(/secret/);
    await expect(verifyWidgetToken(sign(), { ...options, appId: '' })).rejects.toThrow(/vap_/);
  });
});

describe('bridge.otp.verifyToken', () => {
  it('posts the token to the API and returns the result', async () => {
    const requests: Request[] = [];
    const bridge = new Bridge({
      apiKey: 'bk_live_abc',
      baseUrl: 'https://api.example.com',
      fetch: async (input: RequestInfo | URL, init?: RequestInit) => {
        requests.push(new Request(input, init));
        return new Response(
          JSON.stringify({
            valid: true,
            reason: null,
            phone: '+919876543210',
            verification_id: 'otp_06ghatc07nghtrbq1yj7nwyjtm',
            app_id: appId,
            environment: 'live',
            expires_at: '2026-10-07T08:20:00Z',
          }),
          { status: 200, headers: { 'content-type': 'application/json' } },
        );
      },
    });
    const token = sign();
    const res = await bridge.otp.verifyToken(token);
    expect(res).toMatchObject({ valid: true, phone: '+919876543210', app_id: appId });
    expect(requests[0]?.method).toBe('POST');
    expect(new URL(requests[0]?.url ?? '').pathname).toBe('/v1/otp/tokens/verify');
    expect(await requests[0]?.json()).toEqual({ token });
  });
});
