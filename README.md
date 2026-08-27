# ⚡ voltride-orders

Checkout orchestrator for the [VoltRide](https://github.com/coderabbit-demo/voltride-platform) e-bike store. Go, standard library only, in-memory data. Runs on **port 4004**.

One checkout touches four sibling services: fetch the cart from [voltride-cart](https://github.com/coderabbit-demo/voltride-cart), reserve stock in [voltride-inventory](https://github.com/coderabbit-demo/voltride-inventory) (with rollback on any later failure), get the final quote from [voltride-pricing](https://github.com/coderabbit-demo/voltride-pricing), and send the confirmation via [voltride-notifications](https://github.com/coderabbit-demo/voltride-notifications). `clients.go` holds this repo's local copies of all four peers' wire formats. See `AGENTS.md` before changing anything contract-shaped.

## Endpoints

- `GET /health`
- `POST /api/orders` `{ cartId, customerName, customerEmail, shippingAddress }` → 201 order, 409 `insufficient_stock`, 422 `empty_cart`
- `GET /api/orders/:orderId`

## Run

```sh
go run .          # requires Go >= 1.22; PORT/CART_URL/INVENTORY_URL/PRICING_URL/NOTIFICATIONS_URL env vars supported
```

To run the whole VoltRide system, use the scripts in [voltride-platform](https://github.com/coderabbit-demo/voltride-platform).
