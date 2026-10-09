/** Plain questions and answers. Rendered on the page and as FAQPage structured data. */
export const FAQ: { q: string; a: string }[] = [
  {
    q: 'What is Bridge?',
    a: 'An open-source SMS and phone verification platform. Your app calls one API, and Bridge sends each message through an Android phone you paired, or an SMS provider, and reports what the carrier said. Use it hosted or run it on your own server.',
  },
  {
    q: 'How much does Bridge cost?',
    a: 'Hosted Bridge is free for one phone and 300 SMS a month; Pro is $5 a month and Team is $15. Self-hosting is free. Your mobile plan pays for the SMS your phones send.',
  },
  {
    q: 'Will my SIM get blocked?',
    a: 'Bridge keeps each phone under 100 SMS a day by default, within what most operators allow. Add phones to send more, and use them for transactional messages such as codes and alerts, not promotions.',
  },
  {
    q: 'How is Bridge different from Twilio?',
    a: 'Twilio sends from its numbers and bills per message. Bridge sends from the SIMs in your own phones, and can fall back to Twilio, MSG91, Vonage or Plivo when no phone is available.',
  },
  {
    q: 'Does Bridge work on an iPhone?',
    a: 'Not as a gateway: iOS does not let apps send SMS in the background, so the gateway app is Android only. iPhones receive Bridge messages like any other SMS.',
  },
  {
    q: 'Can I send SMS in India with DLT registration?',
    a: 'Yes, through MSG91. Add it as a provider with your approved templates and Bridge passes the template variables. Bridge does not help you bypass DLT.',
  },
  {
    q: 'How do I call Bridge from my code?',
    a: 'With the REST API, the TypeScript SDK (@kroszborg/bridge on npm) or the bridgectl command-line tool.',
  },
];
