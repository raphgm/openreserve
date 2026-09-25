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
        <span class="l-pill"><span class="dot on"></span><span id="l-live">Live on an open ledger</span></span>
        <h1>Save together.<br>Buy safely.<br><span>Everyone can see it's fair.</span></h1>
        <p class="l-lead">Ajo and escrow on an open ledger. Nobody can touch the money. Not even us.</p>
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

    <section class="l-trust" aria-label="Built for">
      <div class="l-marquee"><div>${'<b>Gabis Payments</b><i>✦</i><b>Paynautik</b><i>✦</i><b>SSLabs</b><i>✦</i><b>Ajo groups</b><i>✦</i><b>Online sellers</b><i>✦</i>'.repeat(4)}</div></div>
    </section>

    <section class="l-sec" id="features">
      <h2>Money, minus the drama.</h2>
      <div class="l-bento">
        <article class="big reveal">
          ${icon('ajo')}<h3>Ajo that runs itself</h3><p>See who paid. See who's next.</p>
          <div class="v-ajo"><span style="--h:260">AO</span><span style="--h:190">TK</span><span style="--h:320">FB</span><span class="next" style="--h:150">YOU</span><span class="todo">EM</span><span class="todo">NI</span></div>
        </article>
        <article class="big reveal">
          ${icon('escrow')}<h3>Inspect, then pay</h3><p>Money waits until you're happy.</p>
          <div class="v-esc"><span class="on">Locked</span><i></i><span class="on">Delivered</span><i></i><span>Released</span></div>
        </article>
        <article class="reveal">${icon('send')}<h3>Pay by @username</h3><p>Flat ₦20.</p></article>
        <article class="reveal">${icon('eye')}<h3>Open ledger</h3><p>Every kobo, visible.</p></article>
        <article class="reveal">${icon('shield')}<h3>Your keys</h3><p>No one else can move it.</p></article>
        <article class="reveal">${icon('code')}<h3>For businesses</h3><p>Plug in with one SDK.</p></article>
      </div>
    </section>

    <section class="l-sec" id="how">
      <h2>Escrow in three taps.</h2>
      <ol class="l-steps">
        <li class="reveal"><span>1</span><h3>Lock</h3><p>Buyer pays into escrow.</p></li>
        <li class="reveal"><span>2</span><h3>Check</h3><p>Goods arrive. Inspect them.</p></li>
        <li class="reveal"><span>3</span><h3>Release</h3><p>Seller gets paid. Done.</p></li>
      </ol>
    </section>

    <section class="l-sec l-dev" id="developers">
      <div>
        <h2>Accept ORPay in your app</h2>
        <p class="l-sub left">Checkout, escrow and webhooks. Your logo beside ours.</p>
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
      <h2>Your money. In the open.</h2>
      <p>Free. One minute to start.</p>
      <button class="l-btn light lg" data-start>Create your wallet</button>
    </section>

    <footer class="l-foot">
      <span class="logo" aria-label="ORPay"><span class="logo-pay">Pay</span></span>
      <span>Built on OpenReserve · Test network, no real money yet</span>
      <a href="/explorer.html">Explorer</a>
    </footer>
  </div>`
  const io = 'IntersectionObserver' in window && new IntersectionObserver((es) => es.forEach((e) => e.isIntersecting && (e.target.classList.add('in'), io.unobserve(e.target))), { threshold: 0.15 })
  app.querySelectorAll('.reveal').forEach((el, i) => (io ? ((el.style.transitionDelay = `${(i % 3) * 70}ms`), io.observe(el)) : el.classList.add('in')))
  app.querySelectorAll('.l-bento article').forEach((el) => (el.onpointermove = (e) => {
    const r = el.getBoundingClientRect()
    el.style.setProperty('--mx', `${e.clientX - r.left}px`)
    el.style.setProperty('--my', `${e.clientY - r.top}px`)
  }))
  // Phone mock tilts toward the pointer.
  const mock = app.querySelector('.l-mock'), phone = app.querySelector('.l-phone')
  if (mock && matchMedia('(hover: hover)').matches) {
    mock.onpointermove = (e) => {
      const r = mock.getBoundingClientRect()
      const x = (e.clientX - r.left) / r.width - 0.5, y = (e.clientY - r.top) / r.height - 0.5
      phone.style.transform = `perspective(900px) rotateY(${x * 10}deg) rotateX(${-y * 10}deg)`
    }
    mock.onpointerleave = () => (phone.style.transform = '')
  }
  // Real chain height in the badge.
  fetch('/v1/status').then((r) => r.json()).then((st) => {
    const el = document.getElementById('l-live')
    if (el && st.height > 0) el.textContent = `Live · block ${st.height.toLocaleString()}`
  }).catch(() => {})
  app.querySelectorAll('[data-start]').forEach((b) => (b.onclick = onStart))
  app.querySelectorAll('[data-signin]').forEach((b) => (b.onclick = onSignIn))
}
