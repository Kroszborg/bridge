import type { Metadata } from 'next';
import { LegalPage } from '@/components/legal';
import { CONTACT_EMAIL, DASHBOARD_URL, REPO_URL } from '@/lib/site';

export const metadata: Metadata = {
  title: 'Privacy Policy · Bridge',
  description: 'What hosted Bridge and the Bridge Android app collect, why, and for how long.',
  alternates: { canonical: '/privacy/' },
};

export default function PrivacyPage() {
  return (
    <LegalPage title="Privacy Policy" updated="October 9, 2026">
      <p>
        This policy covers hosted Bridge (the dashboard at{' '}
        <a href={DASHBOARD_URL}>{DASHBOARD_URL.replace(/^https?:\/\//, '')}</a> and its API) and the
        Bridge Android app. Bridge is open source, so everything below can be checked against{' '}
        <a href={REPO_URL}>the code</a>. If you run Bridge on your own server, your data stays on
        that server and this policy does not apply; whoever operates it is responsible for it.
      </p>

      <h2>What we collect</h2>
      <ul>
        <li>
          <strong>Your account:</strong> email address, name (optional), your password, which is
          stored only as an Argon2id hash, and, if you add one, a mobile number you verify. We send
          a one-time code to confirm your email address and number; codes are stored only as hashes
          and expire within 15 minutes.
        </li>
        <li>
          <strong>Messages you send and receive:</strong> phone numbers, message text, delivery
          status and which phone handled each message. You put this data in Bridge to have it
          delivered; we process it only to do that.
        </li>
        <li>
          <strong>Paired phones:</strong> model, Android and app version, battery level, network
          type and SIM details needed to route messages. The app never reads your SMS history,
          contacts, photos or location.
        </li>
        <li>
          <strong>Technical logs:</strong> IP addresses and API request details, to secure the
          service, rate-limit abuse and help you debug your integration.
        </li>
        <li>
          <strong>Billing:</strong> your plan and subscription status. Card and payment details go
          straight to our payment provider and never reach Bridge.
        </li>
      </ul>

      <h2>How long we keep it</h2>
      <ul>
        <li>Message text is erased after 30 days; numbers and statuses stay for your history.</li>
        <li>API request logs are deleted after 14 days.</li>
        <li>
          Account data stays until you delete your account under Account settings, which removes it
          along with workspaces where you are the only member.
        </li>
      </ul>

      <h2>Who processes it for us</h2>
      <ul>
        <li>
          <strong>Amazon Web Services</strong> hosts the servers and database, in Mumbai, India.
        </li>
        <li>
          <strong>Dodo Payments</strong> takes payments as merchant of record and handles invoices
          and tax.
        </li>
        <li>
          <strong>Your email provider and ours</strong> deliver account email such as verification
          codes and password reset links. Codes for your mobile number are sent as SMS through
          Bridge itself, from a phone we operate.
        </li>
        <li>
          <strong>Cloudflare Turnstile</strong>, when switched on, checks that sign-ups, password
          reset requests and repeated sign-in attempts come from a person rather than a bot. While
          the check runs on those pages, Cloudflare processes your IP address and signals from your
          browser and device; Bridge sends Cloudflare your IP address with the check and keeps only
          the result. Cloudflare uses this data only to provide the check.
        </li>
        <li>
          <strong>SMS providers you connect</strong> (MSG91, Twilio, Vonage or Plivo) receive the
          messages you route through them, under their own policies.
        </li>
        <li>
          <strong>Google Firebase Cloud Messaging</strong>, only in the gms build of the Android app
          and only when you turn wake-ups on, wakes the app when a message is waiting. The foss
          build uses UnifiedPush or a persistent connection instead.
        </li>
      </ul>
      <p>
        We do not sell personal data, show ads, or use analytics or tracking scripts. The only
        third-party script is the Turnstile check on those account pages, used for security.
      </p>

      <h2>Cookies</h2>
      <p>
        The dashboard sets a session cookie to keep you signed in and a cookie that remembers your
        last project; your light or dark theme is kept in your browser. There are no advertising or
        third-party cookies.
      </p>

      <h2>The Android app and SMS permissions</h2>
      <p>
        Bridge turns your phone into an SMS gateway. It needs permission to send SMS so it can
        deliver the messages you queue, and to receive SMS so it can forward replies. Forwarding is
        off until you turn it on for a phone; while it is on, the SMS that phone receives are sent
        to its Bridge project, so replies and incoming messages appear in your dashboard and
        webhooks. Turn it on only for a phone whose incoming messages you are happy to share with
        your project. Forwarding stops when you turn it off, unpair the phone or uninstall the app.
      </p>

      <h2>Your rights</h2>
      <p>
        You can see, export through the API, correct and delete your data. To ask a question or make
        a request you cannot do in the dashboard, email{' '}
        <a href={`mailto:${CONTACT_EMAIL}`}>{CONTACT_EMAIL}</a>. We answer within 30 days.
      </p>

      <h2>Changes</h2>
      <p>
        If this policy changes in a way that matters, we will say so in the dashboard before it
        takes effect. Earlier versions are in the public repository&apos;s history.
      </p>
    </LegalPage>
  );
}
