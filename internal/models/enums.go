package models

import (
	"database/sql/driver"
	"errors"
	"fmt"
)

// TeamRole represents the team-level RBAC role.
type TeamRole string

const (
	RoleOwner  TeamRole = "owner"
	RoleAdmin  TeamRole = "admin"
	RoleMember TeamRole = "member"
	RoleViewer TeamRole = "viewer"
)

func (r TeamRole) IsValid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer:
		return true
	}
	return false
}

func (r TeamRole) Value() (driver.Value, error) {
	if !r.IsValid() {
		return nil, fmt.Errorf("invalid TeamRole: %q", r)
	}
	return string(r), nil
}

func (r *TeamRole) Scan(value any) error {
	if value == nil {
		return errors.New("cannot scan nil into TeamRole")
	}
	s, ok := value.(string)
	if !ok {
		if b, ok := value.([]byte); ok {
			s = string(b)
		} else {
			return errors.New("cannot scan non-string value into TeamRole")
		}
	}
	role := TeamRole(s)
	if !role.IsValid() {
		return fmt.Errorf("invalid TeamRole in database: %q", s)
	}
	*r = role
	return nil
}

// IntegrationProvider identifies an external issue tracker / project management provider.
type IntegrationProvider string

const (
	IntegrationGitHub IntegrationProvider = "github"
	IntegrationJira   IntegrationProvider = "jira"
	IntegrationTrello IntegrationProvider = "trello"
)

func (p IntegrationProvider) IsValid() bool {
	switch p {
	case IntegrationGitHub, IntegrationJira, IntegrationTrello:
		return true
	}
	return false
}

func (p IntegrationProvider) Value() (driver.Value, error) {
	if !p.IsValid() {
		return nil, fmt.Errorf("invalid IntegrationProvider: %q", p)
	}
	return string(p), nil
}

func (p *IntegrationProvider) Scan(value any) error {
	if value == nil {
		return errors.New("cannot scan nil into IntegrationProvider")
	}
	s, ok := value.(string)
	if !ok {
		if b, ok := value.([]byte); ok {
			s = string(b)
		} else {
			return errors.New("cannot scan non-string value into IntegrationProvider")
		}
	}
	provider := IntegrationProvider(s)
	if !provider.IsValid() {
		return fmt.Errorf("invalid IntegrationProvider in database: %q", s)
	}
	*p = provider
	return nil
}

// CredentialProvider identifies an external credential target.
type CredentialProvider string

const (
	ProviderGitHub  CredentialProvider = "github"
	ProviderJira    CredentialProvider = "jira"
	ProviderTrello  CredentialProvider = "trello"
	ProviderOIDC    CredentialProvider = "oidc"
	ProviderGeneric CredentialProvider = "generic"
)

func (p CredentialProvider) IsValid() bool {
	switch p {
	case ProviderGitHub, ProviderJira, ProviderTrello, ProviderOIDC, ProviderGeneric:
		return true
	}
	return false
}

func (p CredentialProvider) Value() (driver.Value, error) {
	if !p.IsValid() {
		return nil, fmt.Errorf("invalid CredentialProvider: %q", p)
	}
	return string(p), nil
}

func (p *CredentialProvider) Scan(value any) error {
	if value == nil {
		return errors.New("cannot scan nil into CredentialProvider")
	}
	s, ok := value.(string)
	if !ok {
		if b, ok := value.([]byte); ok {
			s = string(b)
		} else {
			return errors.New("cannot scan non-string value into CredentialProvider")
		}
	}
	provider := CredentialProvider(s)
	if !provider.IsValid() {
		return fmt.Errorf("invalid CredentialProvider in database: %q", s)
	}
	*p = provider
	return nil
}

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusBacklog    TaskStatus = "backlog"
	TaskStatusQueued     TaskStatus = "queued"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusReview     TaskStatus = "review"
	TaskStatusDone       TaskStatus = "done"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
)

func (s TaskStatus) IsValid() bool {
	switch s {
	case TaskStatusBacklog, TaskStatusQueued, TaskStatusInProgress,
		TaskStatusReview, TaskStatusDone, TaskStatusFailed, TaskStatusCancelled:
		return true
	}
	return false
}

func (s TaskStatus) Value() (driver.Value, error) {
	if !s.IsValid() {
		return nil, fmt.Errorf("invalid TaskStatus: %q", s)
	}
	return string(s), nil
}

func (s *TaskStatus) Scan(value any) error {
	if value == nil {
		return errors.New("cannot scan nil into TaskStatus")
	}
	sVal, ok := value.(string)
	if !ok {
		if b, ok := value.([]byte); ok {
			sVal = string(b)
		} else {
			return errors.New("cannot scan non-string value into TaskStatus")
		}
	}
	status := TaskStatus(sVal)
	if !status.IsValid() {
		return fmt.Errorf("invalid TaskStatus in database: %q", sVal)
	}
	*s = status
	return nil
}

// TaskPriority represents queue scheduling priority tiers.
type TaskPriority int

const (
	PriorityUrgent TaskPriority = 100
	PriorityHigh   TaskPriority = 75
	PriorityNormal TaskPriority = 50
	PriorityLow    TaskPriority = 25
)

func (p TaskPriority) IsValid() bool {
	switch p {
	case PriorityUrgent, PriorityHigh, PriorityNormal, PriorityLow:
		return true
	}
	return false
}

func (p TaskPriority) Value() (driver.Value, error) {
	if !p.IsValid() {
		return nil, fmt.Errorf("invalid TaskPriority: %d", p)
	}
	return int64(p), nil
}

func (p *TaskPriority) Scan(value any) error {
	if value == nil {
		return errors.New("cannot scan nil into TaskPriority")
	}
	var intVal int
	switch v := value.(type) {
	case int64:
		intVal = int(v)
	case int:
		intVal = v
	case int32:
		intVal = int(v)
	default:
		return errors.New("cannot scan non-int value into TaskPriority")
	}
	priority := TaskPriority(intVal)
	if !priority.IsValid() {
		return fmt.Errorf("invalid TaskPriority in database: %d", intVal)
	}
	*p = priority
	return nil
}

// AssigneeType designates whether an assignee is an AI agent or human engineer.
type AssigneeType string

const (
	AssigneeAgent AssigneeType = "agent"
	AssigneeHuman AssigneeType = "human"
)

func (t AssigneeType) IsValid() bool {
	switch t {
	case AssigneeAgent, AssigneeHuman:
		return true
	}
	return false
}

func (t AssigneeType) Value() (driver.Value, error) {
	if !t.IsValid() {
		return nil, fmt.Errorf("invalid AssigneeType: %q", t)
	}
	return string(t), nil
}

func (t *AssigneeType) Scan(value any) error {
	if value == nil {
		return errors.New("cannot scan nil into AssigneeType")
	}
	s, ok := value.(string)
	if !ok {
		if b, ok := value.([]byte); ok {
			s = string(b)
		} else {
			return errors.New("cannot scan non-string value into AssigneeType")
		}
	}
	assignee := AssigneeType(s)
	if !assignee.IsValid() {
		return fmt.Errorf("invalid AssigneeType in database: %q", s)
	}
	*t = assignee
	return nil
}

// AttemptStatus represents the execution state of an individual task run.
type AttemptStatus string

const (
	AttemptQueued    AttemptStatus = "queued"
	AttemptRunning   AttemptStatus = "running"
	AttemptSucceeded AttemptStatus = "succeeded"
	AttemptFailed    AttemptStatus = "failed"
	AttemptCancelled AttemptStatus = "cancelled"
)

func (s AttemptStatus) IsValid() bool {
	switch s {
	case AttemptQueued, AttemptRunning, AttemptSucceeded, AttemptFailed, AttemptCancelled:
		return true
	}
	return false
}

func (s AttemptStatus) Value() (driver.Value, error) {
	if !s.IsValid() {
		return nil, fmt.Errorf("invalid AttemptStatus: %q", s)
	}
	return string(s), nil
}

func (s *AttemptStatus) Scan(value any) error {
	if value == nil {
		return errors.New("cannot scan nil into AttemptStatus")
	}
	str, ok := value.(string)
	if !ok {
		if b, ok := value.([]byte); ok {
			str = string(b)
		} else {
			return errors.New("cannot scan non-string value into AttemptStatus")
		}
	}
	status := AttemptStatus(str)
	if !status.IsValid() {
		return fmt.Errorf("invalid AttemptStatus in database: %q", str)
	}
	*s = status
	return nil
}

// WebhookStatus represents the ingestion state of an external webhook payload.
type WebhookStatus string

const (
	WebhookReceived  WebhookStatus = "received"
	WebhookProcessed WebhookStatus = "processed"
	WebhookFailed    WebhookStatus = "failed"
)

func (s WebhookStatus) IsValid() bool {
	switch s {
	case WebhookReceived, WebhookProcessed, WebhookFailed:
		return true
	}
	return false
}

func (s WebhookStatus) Value() (driver.Value, error) {
	if !s.IsValid() {
		return nil, fmt.Errorf("invalid WebhookStatus: %q", s)
	}
	return string(s), nil
}

func (s *WebhookStatus) Scan(value any) error {
	if value == nil {
		return errors.New("cannot scan nil into WebhookStatus")
	}
	str, ok := value.(string)
	if !ok {
		if b, ok := value.([]byte); ok {
			str = string(b)
		} else {
			return errors.New("cannot scan non-string value into WebhookStatus")
		}
	}
	status := WebhookStatus(str)
	if !status.IsValid() {
		return fmt.Errorf("invalid WebhookStatus in database: %q", str)
	}
	*s = status
	return nil
}
