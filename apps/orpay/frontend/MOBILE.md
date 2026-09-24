# ORPay on phones

ORPay ships two ways. Both use the same web code in `src/`.

## 1. Installable web app (PWA)

Nothing to do: the production build includes a manifest, icons and a service
worker. On Android Chrome use **Install app**; on iPhone Safari use
**Share → Add to Home Screen**. The service worker caches only the app
shell; balances and payments always come fresh from the network.

## 2. Native apps (Capacitor): App Store and Play Store

`ios/` and `android/` are Capacitor projects wrapping the same app.

Build the web app against your server, then copy it into the native projects:

```bash
VITE_API_BASE=https://your-domain npm run build:native
```

**iOS** (needs Xcode with the iOS platform installed:
`xcodebuild -downloadPlatform iOS`, or Xcode → Settings → Components):

```bash
npx cap open ios        # then Run in Xcode, or Product → Archive to ship
```

**Android** (needs JDK 21 and Android Studio):

```bash
npx cap open android    # then Run, or Build → Generate Signed Bundle
```

Differences in the native app:

- API calls go to `VITE_API_BASE` (CORS already allows it).
- Paystack / Flutterwave checkout opens in an in-app browser, and the app
  polls until the payment is confirmed.
- Keys are stored the same way as on the web: encrypted with the user's
  password. A native secure-storage/biometric unlock is a good next step.

Publishing needs an Apple Developer account and a Google Play Console
account, a real bundle id (`appId` in `capacitor.config.json` is a
placeholder until the final domain is chosen), and store listings.
Regenerate icons after changing `assets/`: `npx @capacitor/assets generate`.
