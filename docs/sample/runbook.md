# Payments Runbook

## Common alerts

### `PaymentAuthLatencyHigh`
**Symptom:** P99 latency on `POST /charges` exceeds 250ms for 5m.
**First steps:**
1. Check Stripe status at status.stripe.com.
2. Look at the `payments-authorize` CloudWatch dashboard.
3. If DB CPU is > 80%, page the platform on-call.

### `PaymentEventLagHigh`
**Symptom:** Consumer lag on `payments.authorized` exceeds 10k.
**First steps:**
1. Verify Kafka brokers are healthy.
2. Restart the slowest consumer pod and watch lag drain.

## Deploys

Deploys go via Argo CD from the `infra/payments` repo. Roll back with
`argocd app rollback payments-prod <revision>`.
