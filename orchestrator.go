package main

import (
	"errors"
	"fmt"
	"sync"
)

const baseDeliveryDays = 3

type ShippingAddress struct {
	Line1      string `json:"line1"`
	City       string `json:"city"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
}

type CreateOrderRequest struct {
	CartID          string          `json:"cartId"`
	CustomerName    string          `json:"customerName"`
	CustomerEmail   string          `json:"customerEmail"`
	ShippingAddress ShippingAddress `json:"shippingAddress"`
}

type OrderItem struct {
	ProductID      string `json:"productId"`
	Name           string `json:"name"`
	Quantity       int    `json:"quantity"`
	LineTotalCents int    `json:"lineTotalCents"`
}

type Order struct {
	OrderID               string      `json:"orderId"`
	Status                string      `json:"status"`
	Items                 []OrderItem `json:"items"`
	GrandTotalCents       int         `json:"grandTotalCents"`
	EstimatedDeliveryDays int         `json:"estimatedDeliveryDays"`
	ReservationID         string      `json:"reservationId"`
	NotificationID        string      `json:"notificationId"`
}

var (
	ErrCartNotFound = errors.New("cart_not_found")
	ErrEmptyCart    = errors.New("empty_cart")
)

var (
	ordersMu    sync.Mutex
	orders      = map[string]*Order{}
	nextOrderID int
)

func newOrderID() string {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	nextOrderID++
	return fmt.Sprintf("ord-%04x", nextOrderID)
}

func saveOrder(o *Order) {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	orders[o.OrderID] = o
}

func getOrder(orderID string) (*Order, bool) {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	o, ok := orders[orderID]
	return o, ok
}

// placeOrder runs the checkout saga:
//  1. fetch the cart          (cart -> catalog)
//  2. estimate delivery       (inventory, per item, BEFORE reserving)
//  3. reserve stock           (inventory)
//  4. final quote             (pricing -> inventory)
//  5. confirmation email      (notifications -> catalog)
//  6. clear the cart          (cart)
//
// If pricing or notifications fail after the reservation was made, the
// reservation is released so stock is not leaked.
func placeOrder(req CreateOrderRequest) (*Order, error) {
	cart, err := fetchCart(req.CartID)
	if err != nil {
		return nil, err
	}
	if cart == nil {
		return nil, ErrCartNotFound
	}
	if len(cart.Items) == 0 {
		return nil, ErrEmptyCart
	}

	// Delivery estimate must be computed before reserving: reservations
	// decrement stock, which would skew the in-stock check.
	deliveryDays := baseDeliveryDays
	for _, item := range cart.Items {
		stock, err := fetchStock(item.ProductID)
		if err != nil {
			return nil, err
		}
		if stock.StockCount < item.Quantity {
			if eta := stock.RestockEtaDays + baseDeliveryDays; eta > deliveryDays {
				deliveryDays = eta
			}
		}
	}

	orderID := newOrderID()

	reservation, err := reserveStock(orderID, cart.Items)
	if err != nil {
		return nil, err
	}

	quote, err := fetchQuote(cart.Items, cart.PromoCode)
	if err != nil {
		releaseReservation(reservation.ReservationID)
		return nil, err
	}

	lineTotals := map[string]int{}
	for _, line := range quote.LineItems {
		lineTotals[line.ProductID] = line.LineTotalCents
	}

	orderItems := make([]OrderItem, 0, len(cart.Items))
	confItems := make([]confirmationItem, 0, len(cart.Items))
	for _, item := range cart.Items {
		orderItems = append(orderItems, OrderItem{
			ProductID:      item.ProductID,
			Name:           item.Name,
			Quantity:       item.Quantity,
			LineTotalCents: lineTotals[item.ProductID],
		})
		confItems = append(confItems, confirmationItem{
			ProductID:      item.ProductID,
			Quantity:       item.Quantity,
			LineTotalCents: lineTotals[item.ProductID],
		})
	}

	notif, err := sendOrderConfirmation(orderID, req.CustomerName, req.CustomerEmail,
		confItems, quote.GrandTotalCents, deliveryDays)
	if err != nil {
		releaseReservation(reservation.ReservationID)
		return nil, err
	}

	if err := clearCart(req.CartID); err != nil {
		// Non-fatal: the order is already confirmed and paid for.
		fmt.Printf("warning: failed to clear cart %s: %v\n", req.CartID, err)
	}

	order := &Order{
		OrderID:               orderID,
		Status:                "confirmed",
		Items:                 orderItems,
		GrandTotalCents:       quote.GrandTotalCents,
		EstimatedDeliveryDays: deliveryDays,
		ReservationID:         reservation.ReservationID,
		NotificationID:        notif.NotificationID,
	}
	saveOrder(order)
	return order, nil
}
