# Governance

OpenReserve employs an on-chain governance system to manage protocol upgrades, treasury funds, and parameter changes.

## Proposal Lifecycle

1. **Submission**: Any ORP holder can submit a proposal. A minimum deposit of ORP is required to prevent spam.
2. **Deposit Period**: If the initial submitter doesn't meet the minimum deposit, other community members can contribute to the deposit pool until the threshold is met.
3. **Voting Period**: Once the deposit is met, a 14-day voting period begins.
4. **Tallying**: Votes are weighted by the amount of staked ORP.
5. **Execution**: If passed, parameter changes or treasury disbursements execute automatically via the protocol state machine.

## Vote Options
- **Yes**: In favor.
- **No**: Opposed.
- **NoWithVeto**: Strongly opposed (can result in the proposer losing their deposit if a quorum votes this way).
- **Abstain**: Neutral, but contributes to the participation quorum.
