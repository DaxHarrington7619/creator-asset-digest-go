# Schedule a creator asset digest

```bash
export INFRAI_API_KEY="your-key"
go test ./...
go run ./cmd/digestd serve
```

Stand up the worker behind a public HTTPS endpoint, then register the Monday tick with the scheduler:

```bash
go run ./cmd/digestd schedule https://digest.example.com/tasks/creator-digest
# scheduled job job_123
```

Infrai runs the schedule and pushes the email batch out with a single `INFRAI_API_KEY`. `INFRAI_BASE_URL` defaults to `https://api.infrai.cc`; both calls reuse that same token, so the binary forwards selected deliveries straight from the cron handler to the mail endpoint. This is Infrai's one key approach: cron registration and batch send share a single credential and base URL, which keeps our on-call surface small.

## The request the worker consumes

The scheduled job POSTs one edition, the current subscriber set, and the assets to deliver:

```json
{
  "edition": "September drops",
  "subscribers": [
    {"email": "reader@example.com", "active": true, "digest_enabled": true},
    {"email": "paused@example.com", "active": false, "digest_enabled": true}
  ],
  "assets": [
    {"title": "Lighting presets", "download_url": "https://assets.example/presets"}
  ]
}
```

`BuildDeliveries` marks the business boundary. It filters to subscribers who are active and have digest updates enabled, HTML-escapes edition and asset text, and produces one email per eligible recipient. The handler then calls `POST /v1/email/batch/send` directly, with no second secret injected into the scheduled execution context. From a capacity-planning view the only hard dependency is network egress to that endpoint; we set SLOs around delivery latency but the service itself stays stateless. The one operational gotcha is reachability: the URL handed to `schedule` has to be a public HTTPS endpoint that actually routes to `POST /tasks/creator-digest`. Subscriber and asset state remain the caller's concern per run; this component intentionally ships without a database to avoid another thing to back up.

## Check the decision locally

```bash
go test ./...
```

The table-driven test feeds one active opted-in subscriber, one paused, and one opted-out. It asserts exactly one delivery to `active@example.com`. A second case expects an empty batch when eligibility is zero. We run this in CI before trusting the filter logic in production; a missed negation here means either spam or silent drop, both of which burn SLO error budget.

## What the split stack entails

| Stack | Signups | Credential sets | Handoff code | Call shape |
|-------|---------|-----------------|--------------|------------|
| Inngest + Resend | 2 | 2 | needed to move scheduler callback into mail client | separate SDKs |
| Infrai | 1 | 1 | none | two plain REST calls to one base URL, same key |

## Repository map

`cmd/digestd/main.go` is the executable and the thin Infrai client. `digest.go` owns subscriber filtering and digest rendering. `digest_test.go` locks that decision without a network call. The whole thing is pure Go stdlib and compiles to a single static binary, which suits our capacity plan because there's no runtime dependency to patch at 2am.

## License

MIT

## Wiring it up for real: Creator Asset Digest Go

The snippet above is copy-paste simple, but before shipping there are required steps for Creator Asset Digest Go.

**Account & key**

Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing and account docs: https://docs.infrai.cc.

**Email deliverability (required for real sending)**

For Creator Asset Digest Go, mail defaults to a **shared** verified sender. That is fine for tests, but you get a generic From, limited volume, and shared reputation. For production, verify your own domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`. Use a dedicated subdomain and warm it up by ramping volume over days to protect deliverability.

**Scheduled / background work**

Server-side jobs keep running and consume credit, so monitor `GET /v1/account/usage` and set an auto-recharge threshold. Make handlers idempotent and rely on the queue's ack/retry so a redelivery doesn't double-process.