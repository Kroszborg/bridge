Phone screenshots for Google Play and F-Droid. Not generated: take them on a real phone.

Format: PNG, portrait, 1080 x 2400 (any 9:20 or 9:16 phone resolution works; Play accepts
320 to 3840 px per side, 2 to 8 screenshots). Name them 1.png, 2.png, ... in display order.
Both stores ignore this README.txt.

Capture, in this order:

1.png  Gateway tab: status Online, project name, today's count, reliability checklist all green.
2.png  Messages tab: a mix of delivered, sent and one incoming message, Live selected.
3.png  Send tab: a message just sent, its status at Delivered.
4.png  Phones tab: two or three phones, one online and marked "This phone".
5.png  Account tab, or the sign-in screen (server set to hosted Bridge).
6.png  Home screen with the Bridge widget (wide 4x1 or resized taller) showing Online.

Rules:
- Use a dedicated test project with made-up names and numbers (for example +1 555 0100 to
  +1 555 0199). Never show real phone numbers, message text, emails or API keys.
- Light theme for all, or dark for all; do not mix.
- Clean status bar: full battery, no notifications other than Bridge's
  (adb shell settings put global sysui_demo_allowed 1, then the demo-mode commands).
- adb exec-out screencap -p > 1.png captures at the phone's native resolution.
