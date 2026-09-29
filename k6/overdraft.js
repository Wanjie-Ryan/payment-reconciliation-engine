import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'https://ledger.ryanwanjie.com';

export const options = {
  scenarios: {
    concurrent_withdrawals: {
      executor: 'per-vu-iterations',
      vus: 20,
      iterations: 1,
      maxDuration: '30s',
    },
  },
};

export function setup() {
  const owner = `k6-overdraft-${Date.now()}`;
  const acctRes = http.post(`${BASE_URL}/accounts`,
    JSON.stringify({ owner, currency: 'KES' }),
    { headers: { 'Content-Type': 'application/json' } });
  const accountId = acctRes.json('id');

  const depRes = http.post(`${BASE_URL}/deposits`,
    JSON.stringify({ account_id: accountId, amount: 1000, currency: 'KES' }),
    { headers: { 'Content-Type': 'application/json', 'Idempotency-Key': `k6-seed-${Date.now()}` } });
  check(depRes, { 'seed deposit succeeded': (r) => r.status === 201 });

  return { accountId };
}

export default function (data) {
  const res = http.post(`${BASE_URL}/withdrawals`,
    JSON.stringify({ account_id: data.accountId, amount: 100, currency: 'KES' }),
    { headers: {
        'Content-Type': 'application/json',
        // unique per VU/iteration - this must exercise overdraft logic, not idempotency dedup
        'Idempotency-Key': `k6-withdraw-${__VU}-${Date.now()}`,
    }});
  check(res, { 'status is 201 or 422': (r) => r.status === 201 || r.status === 422 });
}

export function teardown(data) {
  console.log(`account under test: ${data.accountId}`);
}
