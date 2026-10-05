package models

import (
	"github.com/go-playground/validator/v10"
)

// Validate is the singleton validator instance.
var Validate = validator.New()

func init() {
	_ = Validate.RegisterValidation("team_role", func(fl validator.FieldLevel) bool {
		return TeamRole(fl.Field().String()).IsValid()
	})
	_ = Validate.RegisterValidation("task_status", func(fl validator.FieldLevel) bool {
		return TaskStatus(fl.Field().String()).IsValid()
	})
	_ = Validate.RegisterValidation("assignee_type", func(fl validator.FieldLevel) bool {
		t := AssigneeType(fl.Field().String())
		return t == AssigneeAgent || t == AssigneeHuman
	})
	_ = Validate.RegisterValidation("task_priority", func(fl validator.FieldLevel) bool {
		p := TaskPriority(fl.Field().Int())
		return p == PriorityUrgent || p == PriorityHigh || p == PriorityNormal || p == PriorityLow
	})
	_ = Validate.RegisterValidation("integration_provider", func(fl validator.FieldLevel) bool {
		return IntegrationProvider(fl.Field().String()).IsValid()
	})
	_ = Validate.RegisterValidation("slug", func(fl validator.FieldLevel) bool {
		val := fl.Field().String()
		if len(val) == 0 {
			return false
		}
		for _, r := range val {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
				return false
			}
		}
		return true
	})
}
