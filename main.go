package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

// POST /api/orders
func handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	if req.CartID == "" || req.CustomerName == "" || req.CustomerEmail == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_required_fields"})
		return
	}

	order, err := placeOrder(req)
	if err != nil {
		var shortStock *InsufficientStock
		switch {
		case errors.As(err, &shortStock):
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":     "insufficient_stock",
				"productId": shortStock.ProductID,
			})
		case errors.Is(err, ErrCartNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "cart_not_found"})
		case errors.Is(err, ErrEmptyCart):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "empty_cart"})
		default:
			log.Printf("checkout failed: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "checkout_failed"})
		}
		return
	}

	log.Printf("order %s confirmed (%d items, total %d cents)",
		order.OrderID, len(order.Items), order.GrandTotalCents)
	writeJSON(w, http.StatusCreated, order)
}

// GET /api/orders/{orderId}
func handleGetOrder(w http.ResponseWriter, r *http.Request) {
	orderID := strings.TrimPrefix(r.URL.Path, "/api/orders/")
	order, ok := getOrder(orderID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "order_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "orders"})
	})
	mux.HandleFunc("POST /api/orders", handleCreateOrder)
	mux.HandleFunc("GET /api/orders/{orderId}", handleGetOrder)

	port := os.Getenv("PORT")
	if port == "" {
		port = "4004"
	}
	log.Printf("orders service listening on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
