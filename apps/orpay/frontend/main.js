const BACKEND_URL = 'http://localhost:4000';
const NODE_URL = 'http://localhost:8080';

// Mock logged-in user state
const currentUser = {
  username: 'alice',
  address: 'orp1mockaddress1234567890abcdef',
  balance: 5000.00
};

document.addEventListener('DOMContentLoaded', () => {
  initApp();
});

function initApp() {
  document.getElementById('display-username').textContent = '@' + currentUser.username;
  document.getElementById('balance-amount').innerHTML = `${currentUser.balance.toFixed(2)} <span class="currency">ORP</span>`;

  // Register mock user to backend
  fetch(`${BACKEND_URL}/api/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: currentUser.username, address: currentUser.address })
  });

  pollStats();
  setInterval(pollStats, 5000);

  const amountInput = document.getElementById('tx-amount');
  const feeCalc = document.getElementById('fee-calc');

  amountInput.addEventListener('input', (e) => {
    const val = parseFloat(e.target.value);
    if (!isNaN(val) && val > 0) {
      const fee = val * 0.001; // 0.1%
      feeCalc.textContent = fee.toFixed(4) + ' ORP';
    } else {
      feeCalc.textContent = '0.00 ORP';
    }
  });

  document.getElementById('send-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const to = document.getElementById('tx-to').value;
    const amount = parseFloat(document.getElementById('tx-amount').value);
    
    if (isNaN(amount) || amount <= 0) return alert('Invalid amount');

    const fee = amount * 0.001;

    try {
      // 1. Resolve username to address if needed
      let receiverAddress = to;
      if (to.startsWith('@')) {
        const username = to.substring(1);
        const res = await fetch(`${BACKEND_URL}/api/resolve?username=${username}`);
        if (!res.ok) throw new Error('User not found');
        const data = await res.json();
        receiverAddress = data.address;
      }

      // In a real app, this is where Wallet SDK is called to sign and send to Node API
      console.log(`Sending ${amount} to ${receiverAddress} with ${fee} fee.`);
      
      // Simulate backend recording the fee burn
      await fetch(`${BACKEND_URL}/api/record_burn`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ amount: fee })
      });

      // Update mock local state
      currentUser.balance -= (amount + fee);
      document.getElementById('balance-amount').innerHTML = `${currentUser.balance.toFixed(2)} <span class="currency">ORP</span>`;
      
      alert(`Successfully sent ${amount} ORP to ${to}!`);
      e.target.reset();
      feeCalc.textContent = '0.00 ORP';
      
      pollStats();
    } catch (err) {
      alert('Error: ' + err.message);
    }
  });
}

async function pollStats() {
  try {
    const res = await fetch(`${BACKEND_URL}/api/stats`);
    if (res.ok) {
      const data = await res.json();
      document.getElementById('total-burned').textContent = data.total_burned.toFixed(2);
    }
  } catch (err) {
    console.error('Failed to fetch stats', err);
  }
}
