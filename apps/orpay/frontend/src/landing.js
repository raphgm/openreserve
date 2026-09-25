// Public landing page for visitors without a wallet on this device.

const check = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12.5 4.5 4.5L19 7.5"/></svg>'
const ic = {
  ajo: '<circle cx="12" cy="12" r="8"/><path d="M12 4a8 8 0 0 1 8 8M12 8v4l3 2"/>',
  escrow: '<path d="M12 3 4.5 6v5.5c0 4.6 3.2 8.2 7.5 9.5 4.3-1.3 7.5-4.9 7.5-9.5V6z"/><path d="m9 12 2 2 4-4"/>',
  send: '<path d="M4 12 20 4l-6 16-3-7z"/><path d="m11 13 9-9"/>',
  eye: '<path d="M2.5 12S6 5 12 5s9.5 7 9.5 7-3.5 7-9.5 7-9.5-7-9.5-7z"/><circle cx="12" cy="12" r="3"/>',
  code: '<path d="m8 8-4 4 4 4M16 8l4 4-4 4M13.5 5l-3 14"/>',
  shield: '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>',
}
const icon = (n) => `<span class="l-ico"><svg viewBox="0 0 24 24" aria-hidden="true">${ic[n]}</svg></span>`

export function renderLanding(app, { onStart, onSignIn, signInLabel }) {
  document.title = 'ORPay: ajo and escrow payments, in the open'
  app.innerHTML = `
  <div class="landing">
    <nav class="l-nav">
      <span class="logo" aria-label="ORPay"><span class="logo-pay">Pay</span></span>
      <div class="l-links">
        <a href="#features">Features</a>
        <a href="#how">How it works</a>
        <a href="#developers">Developers</a>
        <a href="/explorer.html">Explorer</a>
      </div>
      <div class="l-nav-cta">
        <button class="l-btn ghost" data-signin>${signInLabel}</button>
        <button class="l-btn dark" data-start>Get started</button>
      </div>
    </nav>

    <section class="l-hero">
      <div>
        <span class="l-pill"><span class="dot on"></span>Ajo and escrow, on an open ledger</span>
        <h1>Save together.<br>Buy safely.<br><span>Everyone can see it's fair.</span></h1>
        <p class="l-lead">ORPay runs rotating savings pools and inspect-before-you-pay escrow on a public ledger. No one can quietly move the money, not even us.</p>
        <div class="l-cta">
          <button class="l-btn primary lg" data-start>Create a free wallet</button>
          <a class="l-btn ghost lg" href="#how">See how it works</a>
        </div>
        <ul class="l-ticks">
          <li>${check}Flat ₦20 fee</li><li>${check}Keys stay on your phone</li><li>${check}Every kobo traceable</li>
        </ul>
      </div>
      <div class="l-mock" aria-hidden="true">
        <div class="l-phone">
          <div class="l-bal"><small>Naira balance</small><strong>₦248,500.00</strong><span>+₦50,000 ajo payout today</span></div>
          <div class="l-card">
            <div class="l-row"><b>Market women ajo</b><em>Round 4 of 6</em></div>
            <div class="l-bar"><i style="width:66%"></i></div>
            <div class="l-avs"><span style="--h:260">AO</span><span style="--h:190">TK</span><span style="--h:320">FB</span><span style="--h:40">NI</span><span class="next" style="--h:150">YOU</span><span style="--h:220">EM</span></div>
            <small class="l-note">You're next · all 6 paid this round</small>
          </div>
          <div class="l-card l-esc">
            <div class="l-row"><b>iPhone 14 · escrow</b><em class="ok">Delivered</em></div>
            <small class="l-note">₦420,000 held until you inspect it</small>
            <div class="l-btns"><span class="yes">Release</span><span>Dispute</span></div>
          </div>
        </div>
      </div>
    </section>

    <section class="l-trust">
      <span>Built for</span><b>Gabis Payments</b><b>Paynautik</b><b>SSLabs</b><b>and approved partners</b>
    </section>

    <section class="l-sec" id="features">
      <h2>Everything a trusted payment needs</h2>
      <p class="l-sub">Two things people in Nigeria do every day, made safe and transparent.</p>
      <div class="l-grid">
        <article>${icon('ajo')}<h3>Ajo / esusu pools</h3><p>Everyone sees who paid, who has collected and who is next. The pot pays out automatically, and deposits cover anyone who defaults.</p></article>
        <article>${icon('escrow')}<h3>Inspect-before-pay escrow</h3><p>The buyer's money is locked, not sent. The seller gets paid only after the goods are checked, with chat, photos and a neutral arbiter.</p></article>
        <article>${icon('send')}<h3>Send by @username</h3><p>Pay anyone in seconds with a username or QR code, in naira or ORP, for a flat ₦20.</p></article>
        <article>${icon('eye')}<h3>Open ledger</h3><p>Every contribution, payout and release is on a public explorer anyone can check. Transparency, not trust-me.</p></article>
        <article>${icon('shield')}<h3>Your keys, your money</h3><p>Your wallet key is encrypted on your device. Trusted friends can help you recover it, but no company can move your funds.</p></article>
        <article>${icon('code')}<h3>For businesses</h3><p>Add ORPay checkout and escrow to your app with a few lines of code. Your brand sits beside ours.</p></article>
      </div>
    </section>

    <section class="l-sec" id="how">
      <h2>How escrow works</h2>
      <p class="l-sub">No returns, no surprises: buyers read and accept the terms before paying.</p>
      <ol class="l-steps">
        <li><span>1</span><h3>Buyer locks payment</h3><p>After accepting the seller's terms, the money is held on the ledger, not by the seller.</p></li>
        <li><span>2</span><h3>Seller dispatches</h3><p>Both sides chat and share photos. Deadlines are enforced by the ledger itself.</p></li>
        <li><span>3</span><h3>Buyer inspects and releases</h3><p>Happy? Release. Problem? An arbiter reviews the evidence and decides the split.</p></li>
      </ol>
    </section>

    <section class="l-sec l-dev" id="developers">
      <div>
        <h2>Accept ORPay in your app</h2>
        <p class="l-sub left">Create checkouts and escrows from your server, get signed webhooks when money moves, and show "<i>YourBrand</i> × ORPay" with your logo, fetched from your website automatically.</p>
        <button class="l-btn dark" data-start>Request API access</button>
      </div>
      <pre class="l-code"><code><span class="k">import</span> { ORPay } <span class="k">from</span> <span class="s">'@openreserve/orpay'</span>

<span class="k">const</span> orpay = <span class="k">new</span> ORPay({ apiKey: process.env.ORPAY_KEY })

<span class="k">const</span> escrow = <span class="k">await</span> orpay.createEscrow({
  seller: <span class="s">'@gadgethub'</span>, currency: <span class="s">'NGN'</span>,
  description: <span class="s">'iPhone 14, inspect on delivery'</span>,
  milestones: [{ label: <span class="s">'Item'</span>, amount: <span class="s">'420000'</span> }],
  shipByDays: <span class="n">3</span>,
})
<span class="c">// send the buyer to escrow.url</span></code></pre>
    </section>

    <section class="l-final">
      <h2>Start saving and trading with confidence</h2>
      <p>Free to open. Takes a minute. Your keys never leave your device.</p>
      <button class="l-btn light lg" data-start>Create your wallet</button>
    </section>

    <footer class="l-foot">
      <span class="logo" aria-label="ORPay"><span class="logo-pay">Pay</span></span>
      <span>Built on OpenReserve · Test network, no real money yet</span>
      <a href="/explorer.html">Explorer</a>
    </footer>
  </div>`
  app.querySelectorAll('[data-start]').forEach((b) => (b.onclick = onStart))
  app.querySelectorAll('[data-signin]').forEach((b) => (b.onclick = onSignIn))
}
