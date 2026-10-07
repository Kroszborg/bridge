/** Plain questions and answers. Rendered on the page and as FAQPage structured data. */
export const FAQ: { q: string; a: string }[] = [
  {
    q: 'What is Bridge?',
    a: 'Bridge is an open-source SMS and phone verification platform that you run on your own server. Your app calls one REST API. Bridge sends each message through an Android phone you paired, or through an SMS provider, and reports what the carrier said.',
  },
  {
    q: 'How is Bridge different from Twilio?',
    a: 'Twilio is a hosted service that sends from its own numbers and bills per message. Bridge is software you host. By default it sends from the SIM cards in your own Android phones, so the cost is your mobile plan. From 0.5, Bridge can also send through Twilio, MSG91, Vonage or Plivo when no phone is available, behind the same API.',
  },
  {
    q: 'Does Bridge work on an iPhone?',
    a: 'Not as a gateway. iOS does not let apps send SMS in the background, so the gateway app is Android only. iPhones receive Bridge messages like any other SMS and can autofill verification codes, because Bridge can add the domain line Safari uses to offer the code above the keyboard.',
  },
  {
    q: 'Can I send SMS in India with DLT registration?',
    a: 'Bridge does not help you bypass DLT. For DLT-registered traffic, add MSG91 as a provider with your approved templates. Bridge passes template variables, such as the one-time code, to MSG91 instead of free text.',
  },
  {
    q: 'What do I need to self-host Bridge?',
    a: 'A machine with Docker Compose, or the Go binary and a PostgreSQL database. Paired phones need to reach the API over a public HTTPS URL. Each phone needs a SIM and the Bridge Android app: the foss build wakes up through UnifiedPush, the gms build through Firebase Cloud Messaging.',
  },
  {
    q: 'How much does Bridge cost?',
    a: 'Nothing. The server, dashboard and Android app are open source under AGPL-3.0, and the client SDKs are MIT. Your carrier still charges for the SMS your phones send, and providers charge their own rates if you use them as a fallback.',
  },
  {
    q: 'Is Bridge ready for production?',
    a: 'Not yet. Bridge is pre-release. Sending and delivery reports are verified on a realme phone running Android 14 on Airtel, and more devices are being tested before a stable release.',
  },
  {
    q: 'How do I call Bridge from my code?',
    a: 'Through the REST API, described by an OpenAPI document that every Bridge server serves at /openapi.json. There is also a zero-dependency TypeScript SDK, @kroszborg/bridge, and a command-line tool, bridgectl.',
  },
];
