package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Orders keeps its own local structs mirroring each peer's wire format.
// These are hand-maintained copies — if a peer changes its contract,
// nothing here fails to compile; checkout just breaks at runtime.

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var (
	cartURL          = envOr("CART_URL", "http://localhost:4002")
	inventoryURL     = envOr("INVENTORY_URL", "http://localhost:4003")
	pricingURL       = envOr("PRICING_URL", "http://localhost:4005")
	notificationsURL = envOr("NOTIFICATIONS_URL", "http://localhost:4006")

	httpClient = &http.Client{Timeout: 10 * time.Second}
)

// --- cart ---------------------------------------------------------------

type CartItem struct {
	ProductID      string `json:"productId"`
	Name           string `json:"name"`
	Quantity       int    `json:"quantity"`
	BasePriceCents int    `json:"basePriceCents"`
}

type Cart struct {
	CartID    string     `json:"cartId"`
	Items     []CartItem `json:"items"`
	PromoCode *string    `json:"promoCode"`
}

func fetchCart(cartID string) (*Cart, error) {
	resp, err := httpClient.Get(fmt.Sprintf("%s/api/carts/%s", cartURL, cartID))
	if err != nil {
		return nil, fmt.Errorf("cart service unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cart service returned %d", resp.StatusCode)
	}
	var cart Cart
	if err := json.NewDecoder(resp.Body).Decode(&cart); err != nil {
		return nil, err
	}
	return &cart, nil
}

func clearCart(cartID string) error {
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/carts/%s", cartURL, cartID), nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// --- inventory ------------------------------------------------------------

type StockRecord struct {
	ProductID      string `json:"productId"`
	StockCount     int    `json:"stockCount"`
	RestockEtaDays int    `json:"restockEtaDays"`
}

type reservationItem struct {
	ProductID string `json:"productId"`
	Quantity  int    `json:"quantity"`
}

type ReservationResponse struct {
	ReservationID string `json:"reservationId"`
	Status        string `json:"status"`
}

type InsufficientStock struct {
	ProductID string
}

func (e *InsufficientStock) Error() string {
	return "insufficient stock for " + e.ProductID
}

func fetchStock(productID string) (*StockRecord, error) {
	resp, err := httpClient.Get(fmt.Sprintf("%s/api/stock/%s", inventoryURL, productID))
	if err != nil {
		return nil, fmt.Errorf("inventory service unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("inventory service returned %d", resp.StatusCode)
	}
	var rec StockRecord
	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func reserveStock(orderID string, items []CartItem) (*ReservationResponse, error) {
	payload := map[string]any{"orderId": orderID}
	resItems := make([]reservationItem, 0, len(items))
	for _, item := range items {
		resItems = append(resItems, reservationItem{ProductID: item.ProductID, Quantity: item.Quantity})
	}
	payload["items"] = resItems

	body, _ := json.Marshal(payload)
	resp, err := httpClient.Post(inventoryURL+"/api/reservations", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("inventory service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		var conflict struct {
			ProductID string `json:"productId"`
		}
		json.NewDecoder(resp.Body).Decode(&conflict)
		return nil, &InsufficientStock{ProductID: conflict.ProductID}
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("inventory service returned %d", resp.StatusCode)
	}

	var res ReservationResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	// Inventory reports "reserved" for a successful hold; anything else
	// means the stock is not actually committed to this order.
	if res.Status != "reserved" {
		return nil, fmt.Errorf("unexpected reservation status %q", res.Status)
	}
	return &res, nil
}

func releaseReservation(reservationID string) {
	req, _ := http.NewRequest(http.MethodDelete,
		fmt.Sprintf("%s/api/reservations/%s", inventoryURL, reservationID), nil)
	if resp, err := httpClient.Do(req); err == nil {
		resp.Body.Close()
	}
}

// --- pricing ----------------------------------------------------------------

type QuoteLineItem struct {
	ProductID      string `json:"productId"`
	UnitPriceCents int    `json:"unitPriceCents"`
	Quantity       int    `json:"quantity"`
	LineTotalCents int    `json:"lineTotalCents"`
}

type Quote struct {
	LineItems       []QuoteLineItem `json:"lineItems"`
	GrandTotalCents int             `json:"grandTotalCents"`
}

func fetchQuote(items []CartItem, promoCode *string) (*Quote, error) {
	type quoteItem struct {
		ProductID      string `json:"productId"`
		BasePriceCents int    `json:"basePriceCents"`
		Quantity       int    `json:"quantity"`
	}
	quoteItems := make([]quoteItem, 0, len(items))
	for _, item := range items {
		quoteItems = append(quoteItems, quoteItem{
			ProductID:      item.ProductID,
			BasePriceCents: item.BasePriceCents,
			Quantity:       item.Quantity,
		})
	}
	body, _ := json.Marshal(map[string]any{"items": quoteItems, "promoCode": promoCode})
	resp, err := httpClient.Post(pricingURL+"/api/quotes", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("pricing service unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing service returned %d", resp.StatusCode)
	}
	var quote Quote
	if err := json.NewDecoder(resp.Body).Decode(&quote); err != nil {
		return nil, err
	}
	return &quote, nil
}

// --- notifications -----------------------------------------------------------

type confirmationItem struct {
	ProductID      string `json:"productId"`
	Quantity       int    `json:"quantity"`
	LineTotalCents int    `json:"lineTotalCents"`
}

type NotificationResponse struct {
	NotificationID string `json:"notificationId"`
	Status         string `json:"status"`
}

func sendOrderConfirmation(orderID, customerName, customerEmail string,
	items []confirmationItem, grandTotalCents, estimatedDeliveryDays int) (*NotificationResponse, error) {

	body, _ := json.Marshal(map[string]any{
		"orderId":               orderID,
		"customerEmail":         customerEmail,
		"customerName":          customerName,
		"items":                 items,
		"grandTotalCents":       grandTotalCents,
		"estimatedDeliveryDays": estimatedDeliveryDays,
	})
	resp, err := httpClient.Post(notificationsURL+"/api/notifications/order-confirmation",
		"application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("notifications service unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("notifications service returned %d", resp.StatusCode)
	}
	var notif NotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&notif); err != nil {
		return nil, err
	}
	return &notif, nil
}
