import type { Metadata } from 'next';
import { LegalPage } from '@/components/legal';
import { CONTACT_EMAIL, MAKER, REPO_URL } from '@/lib/site';

export const metadata: Metadata = {
  title: 'Terms of Service · Bridge',
  description: 'The terms for using hosted Bridge.',
  alternates: { canonical: '/terms/' },
};

export default function TermsPage() {
  return (
    <LegalPage title="Terms of Service" updated="October 8, 2026">
      <p>
        These terms cover hosted Bridge, run by {MAKER.name}. By creating an account you agree to
        them. The Bridge software itself is open source under the AGPL-3.0 (server, dashboard and
        app) and MIT (SDKs) licenses; those licenses, not these terms, govern running{' '}
        <a href={REPO_URL}>the code</a> yourself.
      </p>

      <h2>Your account</h2>
      <p>
        Keep your password and API keys safe; you are responsible for what happens with them. You
        must be old enough to enter a contract where you live, and the details you give us must be
        accurate.
      </p>

      <h2>Acceptable use</h2>
      <p>You are responsible for the messages you send through Bridge. You may not use it to:</p>
      <ul>
        <li>send spam, or messages to people who have not agreed to receive them;</li>
        <li>send fraudulent, harassing, illegal or deceptive content, including phishing;</li>
        <li>break the rules of your mobile carrier, SMS provider, or telecom regulator;</li>
        <li>overload, probe or attack the service or other customers.</li>
      </ul>
      <p>
        Bridge honors opt-outs (for example a recipient replying STOP), and you must too. We may
        suspend an account that breaks these rules, and will tell you why unless the law prevents
        it.
      </p>

      <h2>Plans and payment</h2>
      <p>
        Paid plans are billed monthly in advance through Dodo Payments, our merchant of record, and
        renew until you cancel. You can cancel at any time from Billing; your plan stays until the
        end of the paid period and is not refunded for the remainder, unless the law requires
        otherwise. When a plan&apos;s monthly allowance is used up, live sending pauses until the
        next month or an upgrade. We will give at least 30 days&apos; notice before raising the
        price of a plan you are on.
      </p>
      <p>
        Bridge sends SMS through phones and SIMs you own, or through SMS providers you connect.
        Carrier and provider charges for those messages are yours.
      </p>

      <h2>Your data</h2>
      <p>
        You own the data you put into Bridge. We use it only to run the service for you, as
        described in the <a href="/privacy/">Privacy Policy</a>. You can delete your account and
        data at any time.
      </p>

      <h2>Availability</h2>
      <p>
        We work to keep Bridge available, and publish its health on the status page, but the service
        is provided as is, without guaranteed uptime. SMS delivery depends on phones, carriers and
        providers outside our control.
      </p>

      <h2>Liability</h2>
      <p>
        To the extent the law allows, we are not liable for indirect or consequential losses, and
        our total liability for any claim is limited to the amount you paid us in the 12 months
        before it.
      </p>

      <h2>Changes and ending</h2>
      <p>
        We may update these terms and will announce changes that matter in the dashboard before they
        take effect. You can stop using Bridge at any time by deleting your account. Questions:
        email <a href={`mailto:${CONTACT_EMAIL}`}>{CONTACT_EMAIL}</a>.
      </p>
    </LegalPage>
  );
}
