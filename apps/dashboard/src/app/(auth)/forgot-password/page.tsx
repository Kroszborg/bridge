import type { Metadata } from 'next';
import { Suspense } from 'react';
import { ForgotPassword } from './forgot-password';

export const metadata: Metadata = { title: 'Reset your password' };

export default function ForgotPasswordPage() {
  return (
    <Suspense>
      <ForgotPassword />
    </Suspense>
  );
}
