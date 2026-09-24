// Differences when ORPay runs as a native app (Capacitor) instead of a web page.
import { Browser } from '@capacitor/browser'

export const isNative = () => !!window.Capacitor?.isNativePlatform?.()

// openCheckout sends the user to a payment provider's hosted checkout. On the
// web the page navigates and the provider redirects back; in the native app
// it opens an in-app browser and the caller polls for the result instead.
// Returns true if the caller should start polling now.
export async function openCheckout(url) {
  if (isNative()) {
    await Browser.open({ url, presentationStyle: 'popover' })
    return true
  }
  location.assign(url)
  return false
}
