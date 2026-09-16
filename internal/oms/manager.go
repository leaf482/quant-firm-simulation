// Package oms manages in-memory order identities and explicit lifecycle changes.
package oms

import (
	"fmt"
	"math"
	"sort"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

// Manager is single-threaded and returns order copies, never mutable references.
// IDs are deterministic and unique within a manager run. The zero value is usable.
type Manager struct {
	orders   map[domain.OrderID]domain.Order
	intents  map[domain.IntentID]domain.OrderID
	sequence uint64
}

func NewManager() *Manager { return &Manager{} }

// Get returns an order copy for recovery validation and inspection.
func (m *Manager) Get(id domain.OrderID) (domain.Order, error) {
	o, ok := m.orders[id]
	if !ok {
		return domain.Order{}, fmt.Errorf("OMS: unknown order ID %q", id)
	}
	return o, nil
}

// Orders returns copies in deterministic OrderID lexical order.
func (m *Manager) Orders() []domain.Order {
	orders := make([]domain.Order, 0, len(m.orders))
	for _, o := range m.orders {
		orders = append(orders, o)
	}
	sort.Slice(orders, func(i, j int) bool { return orders[i].OrderID < orders[j].OrderID })
	return orders
}

// Create assumes the caller has obtained risk approval. Identical retries return
// the current order; conflicting payloads for an existing IntentID are rejected.
// Timestamps representing the same instant are considered equal.
func (m *Manager) Create(intent domain.OrderIntent) (domain.Order, error) {
	if err := intent.Validate(); err != nil {
		return domain.Order{}, fmt.Errorf("OMS intent: %w", err)
	}
	if id, ok := m.intents[intent.IntentID]; ok {
		o := m.orders[id]
		if o.Symbol != intent.Symbol || o.Side != intent.Side || o.Quantity != intent.Quantity || !o.CreatedAt.Equal(intent.Timestamp) {
			return domain.Order{}, fmt.Errorf("OMS: conflicting payload for intent %q", intent.IntentID)
		}
		return o, nil
	}
	if m.sequence == math.MaxUint64 {
		return domain.Order{}, fmt.Errorf("OMS: order ID sequence exhausted")
	}
	o := domain.Order{
		OrderID:  domain.OrderID(fmt.Sprintf("order-%d", m.sequence+1)),
		IntentID: intent.IntentID, Symbol: intent.Symbol, Side: intent.Side,
		Quantity: intent.Quantity, Status: domain.OrderNew, CreatedAt: intent.Timestamp,
	}
	if err := o.Validate(); err != nil {
		return domain.Order{}, fmt.Errorf("OMS order: %w", err)
	}
	if m.orders == nil {
		m.orders = make(map[domain.OrderID]domain.Order)
		m.intents = make(map[domain.IntentID]domain.OrderID)
	}
	m.orders[o.OrderID] = o
	m.intents[intent.IntentID] = o.OrderID
	m.sequence++
	return o, nil
}

// Transition changes lifecycle state only; SUBMITTED does not call a broker.
// Repeating the current state is an idempotent success, including terminal states.
func (m *Manager) Transition(id domain.OrderID, status domain.OrderStatus) (domain.Order, error) {
	o, ok := m.orders[id]
	if !ok {
		return domain.Order{}, fmt.Errorf("OMS: unknown order ID %q", id)
	}
	if err := status.Validate(); err != nil {
		return domain.Order{}, fmt.Errorf("OMS: %w", err)
	}
	if o.Status == status {
		return o, nil
	}
	allowed := (o.Status == domain.OrderNew && (status == domain.OrderSubmitted || status == domain.OrderRejected)) ||
		(o.Status == domain.OrderSubmitted && (status == domain.OrderCancelled || status == domain.OrderRejected || status == domain.OrderFilled))
	if !allowed {
		return domain.Order{}, fmt.Errorf("OMS: invalid transition %s -> %s for %q", o.Status, status, id)
	}
	o.Status = status
	m.orders[id] = o
	return o, nil
}
