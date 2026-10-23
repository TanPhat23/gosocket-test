package dto

// WebhookResultDTO represents the outcome of processing an inbound webhook event.
type WebhookResultDTO struct {
	Status     string `json:"status"` // "processed", "duplicate", "ignored"
	DeliveryID string `json:"delivery_id,omitempty"`
	EventID    string `json:"event_id,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}
