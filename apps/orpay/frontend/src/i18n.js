// Languages. Interface text is translated as it is drawn: any text node or
// placeholder whose whole text is a known phrase is swapped for the chosen
// language. User content (names, notes, chats) is never a full match for
// these interface phrases in practice, and amounts are never touched.
//
// English and Nigerian Pidgin ship today. Yoruba, Igbo, Hausa, Swahili and
// French are listed as coming soon: they need review by native speakers
// before real users see them.

const KEY = 'orpay.lang'

export const LANGS = [
  { id: 'en', name: 'English' },
  { id: 'pcm', name: 'Pidgin (Naijá)' },
]
export const COMING = ['Yorùbá', 'Igbo', 'Hausa', 'Kiswahili', 'Français']

const PCM = {
  // Home and navigation
  'Good morning': 'Good morning o', 'Good afternoon': 'Good afternoon o', 'Good evening': 'Good evening o',
  'Welcome to ORPay': 'Welcome to ORPay', 'Total balance': 'All your money', 'Send': 'Send', 'Receive': 'Collect',
  'Escrow': 'Escrow', 'Ajo': 'Ajo', 'Home': 'Home', 'Pools': 'Ajo', 'Developers': 'Developers',
  'Recent activity': 'Wetin happen recently', 'See all': 'See everything', 'Close': 'Close am',
  'No payments yet': 'No payment never land', 'Add money or ask a friend to pay you.': 'Add money or make person pay you.',
  'Add money': 'Add money', 'Withdraw': 'Comot money', 'Today': 'Today', 'Yesterday': 'Yesterday',
  // Send and receive
  'Send money': 'Send money', 'Receive money': 'Collect money', 'To': 'Who you dey send am give',
  'Amount (₦)': 'How much (₦)', 'Review': 'Check am well', 'Scan QR': 'Scan QR',
  '@username or address': '@username or address', 'Note (optional, public)': 'Note (if you like, everybody go see am)',
  'Confirm and send': 'Confirm make e go', 'Cancel': 'Leave am',
  // Unlock, sign in
  'Welcome back': 'You don come back!', 'Password': 'Password', 'Unlock': 'Open am', 'Forgot password?': 'You forget password?',
  'Use a different wallet': 'Use another wallet', 'Enter your password': 'Put your password', 'Sign in': 'Enter',
  'Sign out': 'Comot (lock am)', 'Create a wallet': 'Open new wallet', 'Get started': 'Make we start',
  'Your keys are encrypted and never leave this device.': 'Your key dey locked for this phone, e no dey comot.',
  'Six sign-in words': 'Your 6 sign-in words', 'Username': 'Username',
  // Profile menu
  'My address & QR': 'My address & QR', 'Share to get paid': 'Share am make dem pay you',
  'Six-word sign-in': '6-word sign-in', 'Open this wallet on another device': 'Open this wallet for another phone',
  'Recovery words': 'Recovery words', 'Your offline backup': 'Your backup wey no dey online',
  'Notifications': 'Notifications', 'Payments, ajo turns, escrow updates': 'Payment, ajo turn, escrow update',
  'Theme': 'Colour', 'Language': 'Language', 'Invite friends': 'Invite your people',
  'Share your link and see who joined': 'Share your link, see who don join',
  'Lock the wallet on this device': 'Lock the wallet for this phone', 'Remove from this device': 'Remove am from this phone',
  'Face ID / fingerprint': 'Face ID / fingerprint', 'Claim a username': 'Take username',
  // Ajo
  'Savings pools': 'Ajo dem', 'New savings pool': 'New ajo', 'Pool name': 'Ajo name',
  'Contribution per round': 'How much each person go pay', 'Rounds': 'How often', 'Join pool': 'Join this ajo',
  'Autopay': 'Auto-pay', 'Turn on autopay': 'Make e dey pay by itself', 'Members & payout order': 'Members and who go collect first',
  'Receiving now': 'Na im dey collect now', 'Next': 'Next person', 'Paid': 'Don pay', 'Not paid': 'Never pay',
  'Rotation': 'Normal rotation', 'Lottery': 'Draw lots', 'Bidding': 'Bidding', 'Savings goal': 'Save for target',
  'Delete this circle': 'Cancel this ajo',
  // Escrow
  'Create an escrow link': 'Make escrow link', 'Selling online?': 'You dey sell online?', 'Arbiters': 'Judges',
  'Open a dispute': 'Report wahala', 'Mark dispatched / delivered': 'I don send am', 'Show pickup QR': 'Show pickup QR',
  'Check before you release': 'Check everything before you release', 'Status': 'Where e reach',
  'Escrow funded': 'Money don lock for escrow', 'Shipped': 'Dem don send am', 'Funds released': 'Money don release',
  'Inspected and approved': 'Buyer don check am, e good',
  // Activity
  'Escrow claimed by seller': 'Seller don collect escrow', 'Escrow dispute opened': 'Escrow wahala don start',
  'Escrow dispute resolved': 'Judge don settle the wahala', 'Escrow milestone released': 'Escrow money don release',
  'Escrow opened with you': 'Person open escrow with you', 'Escrow refunded': 'Escrow money don return',
  'Locked funds in escrow': 'You lock money for escrow', 'Marked escrow dispatched': 'You talk say you don send am',
  'Released an escrow milestone': 'You release escrow money', 'Seller dispatched': 'Seller don send am',
  'Created a savings pool': 'You start new ajo', 'Joined a savings pool': 'You join ajo',
  'Pool contribution': 'Ajo payment', 'Received pool payout': 'You collect ajo money',
  'Added money': 'You add money', 'Withdrawal to bank': 'You comot money go bank', 'Card payment received': 'Card payment don land',
  // Trust and safety
  'Your trust profile': 'Your trust record', 'Building trust': 'Trust dey grow', 'Trusted': 'Person wey dem fit trust',
  'Excellent': 'Correct person', 'New': 'New', '⚠ Check before you pay': '⚠ Check well before you pay',
  '⚠ Be careful': '⚠ Shine your eye',
  'Built only from payments recorded on OpenReserve. It can\'t be bought or edited.': 'Na only payment wey OpenReserve record na im build am. Nobody fit buy am or change am.',
}

const DICTS = { pcm: PCM }

export const getLang = () => {
  try {
    return localStorage.getItem(KEY) || 'en'
  } catch {
    return 'en'
  }
}

export function setLang(id) {
  try {
    localStorage.setItem(KEY, id)
  } catch {}
  location.reload() // simplest way to redraw everything in the new language
}

// Swap known phrases under root.
function translate(root, dict) {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const raw = n.nodeValue
    const key = raw.trim()
    if (key && dict[key] && dict[key] !== key) n.nodeValue = raw.replace(key, dict[key])
  }
  if (root.querySelectorAll)
    root.querySelectorAll('[placeholder]').forEach((el) => {
      const v = dict[el.getAttribute('placeholder')]
      if (v) el.setAttribute('placeholder', v)
    })
}

export function startI18n() {
  const lang = getLang()
  document.documentElement.lang = lang === 'pcm' ? 'pcm' : 'en'
  const dict = DICTS[lang]
  if (!dict) return
  translate(document.body, dict)
  new MutationObserver((muts) => {
    for (const m of muts) m.addedNodes.forEach((n) => n.nodeType === 1 ? translate(n, dict) : n.nodeType === 3 && n.parentNode && translate(n.parentNode, dict))
  }).observe(document.body, { childList: true, subtree: true })
}
