package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Webhook is an endpoint that receives the events of deneb.
type Webhook struct {
	ID        string
	URL       string
	Secret    string
	CreatedAt time.Time
}

// The states of a delivery of an event to a webhook.
const (
	DeliveryPending   = "pending"
	DeliveryDelivered = "delivered"
	DeliveryFailed    = "failed"
)

// Delivery is one event that deneb sends, or tries to send, to one webhook.
type Delivery struct {
	ID             string
	WebhookID      string
	Event          string
	Payload        string
	Status         string
	Attempts       int
	ResponseStatus int
	LastError      string
	CreatedAt      time.Time
	DeliveredAt    *time.Time
}

// CreateWebhook adds an endpoint for the events.
func (s *Store) CreateWebhook(ctx context.Context, id, url, secret string, now time.Time) (Webhook, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO webhooks (id, url, secret, created_at) VALUES (?, ?, ?, ?)`, id, url, secret, millis(now))
	return Webhook{ID: id, URL: url, Secret: secret, CreatedAt: now}, err
}

// ListWebhooks returns every endpoint, the newest first.
// WebhookExists tells whether a webhook exists.
func (s *Store) WebhookExists(ctx context.Context, id string) (bool, error) {
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM webhooks WHERE id = ?)`, id).Scan(&found)
	return found, err
}

func (s *Store) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, url, secret, created_at FROM webhooks ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hooks := []Webhook{}
	for rows.Next() {
		var hook Webhook
		var created int64
		if err := rows.Scan(&hook.ID, &hook.URL, &hook.Secret, &created); err != nil {
			return nil, err
		}
		hook.CreatedAt = fromMillis(created)
		hooks = append(hooks, hook)
	}
	return hooks, rows.Err()
}

// DeleteWebhook removes an endpoint and its deliveries.
func (s *Store) DeleteWebhook(ctx context.Context, id string) error {
	return expectRow(s.db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id))
}

// QueueEvent creates a delivery of the event to every webhook, and a job for each.
// The IDs of the deliveries come from newID.
func (s *Store) QueueEvent(ctx context.Context, event, payload string, newID func() string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The webhooks are read in the transaction, so that one deleted meanwhile cannot
	// fail the deliveries to the others.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM webhooks`)
	if err != nil {
		return err
	}
	var hooks []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		hooks = append(hooks, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, hook := range hooks {
		id := newID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO webhook_deliveries (id, webhook_id, event, payload, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, hook, event, payload, DeliveryPending, millis(now)); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, JobWebhook, id, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeliveryWithWebhook returns a delivery and the endpoint that it goes to.
func (s *Store) DeliveryWithWebhook(ctx context.Context, id string) (Delivery, Webhook, error) {
	var delivery Delivery
	var hook Webhook
	var created, hookCreated int64
	var delivered sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT d.id, d.webhook_id, d.event, d.payload, d.status, d.attempts, d.response_status, d.last_error, d.created_at, d.delivered_at,
		        w.id, w.url, w.secret, w.created_at
		 FROM webhook_deliveries d JOIN webhooks w ON w.id = d.webhook_id WHERE d.id = ?`, id).
		Scan(&delivery.ID, &delivery.WebhookID, &delivery.Event, &delivery.Payload, &delivery.Status, &delivery.Attempts,
			&delivery.ResponseStatus, &delivery.LastError, &created, &delivered, &hook.ID, &hook.URL, &hook.Secret, &hookCreated)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, Webhook{}, ErrNotFound
	}
	if err != nil {
		return Delivery{}, Webhook{}, err
	}
	delivery.CreatedAt = fromMillis(created)
	delivery.DeliveredAt = nullableTime(delivered)
	hook.CreatedAt = fromMillis(hookCreated)
	return delivery, hook, nil
}

// RecordDelivery records the result of an attempt to send a delivery.
func (s *Store) RecordDelivery(ctx context.Context, id, status string, responseStatus int, lastError string, now time.Time) error {
	var delivered any
	if status == DeliveryDelivered {
		delivered = millis(now)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, attempts = attempts + 1, response_status = ?, last_error = ?, delivered_at = ? WHERE id = ?`,
		status, responseStatus, lastError, delivered, id)
	return err
}

// ListDeliveries returns the latest deliveries to a webhook.
// DeleteOldDeliveries removes the deliveries that ended before a time.
func (s *Store) DeleteOldDeliveries(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE status IN (?, ?) AND created_at < ?`,
		DeliveryDelivered, DeliveryFailed, millis(before))
	return err
}

func (s *Store) ListDeliveries(ctx context.Context, webhookID string, limit int) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, webhook_id, event, payload, status, attempts, response_status, last_error, created_at, delivered_at
		 FROM webhook_deliveries WHERE webhook_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deliveries := []Delivery{}
	for rows.Next() {
		var delivery Delivery
		var created int64
		var delivered sql.NullInt64
		if err := rows.Scan(&delivery.ID, &delivery.WebhookID, &delivery.Event, &delivery.Payload, &delivery.Status, &delivery.Attempts,
			&delivery.ResponseStatus, &delivery.LastError, &created, &delivered); err != nil {
			return nil, err
		}
		delivery.CreatedAt = fromMillis(created)
		delivery.DeliveredAt = nullableTime(delivered)
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}
