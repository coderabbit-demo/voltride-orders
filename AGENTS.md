# AGENTS.md — voltride-orders

Part of VoltRide, a multi-repo microservices demo (see the `voltride-platform` repo for the system map). Every repo hand-maintains local copies of its peers' contracts — there is **no shared types package anywhere in VoltRide**, and nothing must ever change that. `clients.go` holds this repo's local structs for four peers' wire formats; **Go decodes missing or renamed JSON keys silently as zero values**, so drift here corrupts checkouts without a single error.

## Contracts this repo PRODUCES

| Contract | Consumer repo | Consumer file | Failure mode if changed |
|---|---|---|---|
| Order response (`orderId`, `status`, `items[]`, `grandTotalCents`, `estimatedDeliveryDays`, …) | voltride-frontend | `src/api/orders.ts` | confirmation page breaks |
| 409 `insufficient_stock` / 422 `empty_cart` error bodies | voltride-frontend | `src/api/orders.ts` (`CheckoutError`) | checkout error UX breaks |
| Order-confirmation payload POSTed to notifications | voltride-notifications | `main.py` (`OrderConfirmationRequest`, strict Pydantic) | any drift → 422 → **every checkout fails at the final hop** |

## Contracts this repo CONSUMES (local structs in `clients.go`)

| Producer repo | Contract | Notes |
|---|---|---|
| voltride-cart | cart response (`cartId`, `items`, `promoCode`) + `DELETE /api/carts/:id` | promoCode forwarded to pricing |
| voltride-inventory | stock record + reservation API | requires exact status string `"reserved"`; releases the reservation on any post-reservation failure |
| voltride-pricing | quote (`lineItems`, `grandTotalCents`) | `grandTotalCents` is the amount charged |
| voltride-notifications | 201 response (`notificationId`, `status`) | |

**Changing any produced shape (including the payload sent to notifications) is a breaking change for the repos above** — it cannot be fixed in this PR; open coordinated PRs and link them. When a producer repo changes, update the local structs here. Preserve the checkout saga's rollback: every failure after `reserveStock` must call `releaseReservation`.

## Conventions

- Peer URLs via env vars (`CART_URL`, `INVENTORY_URL`, `PRICING_URL`, `NOTIFICATIONS_URL`) with localhost defaults; money is integer cents.
- Standard library only; no external Go deps.
- Verify with: `go vet ./... && go build -o /dev/null .`, then a full curl checkout against the running system.
