# Payments Service

The Payments service handles charge authorization, capture, and refunds for the
storefront. It exposes a gRPC API at `payments.internal:9090` and a REST gateway
at `https://api.example.com/payments/v1`.

## Owners

- Primary on-call: payments-oncall@example.com
- Tech lead: jane.doe@example.com

## Dependencies

- PostgreSQL 15 (RDS, multi-AZ) — transactional ledger
- Stripe — upstream PSP
- Kafka — emits `payments.authorized` and `payments.captured` events

## SLOs

| SLO | Target | Window |
|---|---|---|
| Availability | 99.95% | 30d |
| P99 latency | < 250ms | 7d |
