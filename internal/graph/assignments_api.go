package graph

import (
	"fmt"
	"time"
)

type Assignment struct {
	ID          string
	DisplayName string
	ClassID     string
	DueDateTime time.Time
	Status      string
	IsCompleted bool
}

func (c *Client) FetchAssignments() ([]Assignment, error) {
	if c.EduToken == "" {
		return nil, fmt.Errorf("EDU_TOKEN not configured")
	}
	if c.EduCookie == "" {
		return nil, fmt.Errorf("EDU_COOKIE not configured.\nCapture it from DevTools > Network > assignments.edu.cloud.microsoft > Cookie header")
	}

	return nil, fmt.Errorf("Education Assignments API blocked by WAF (TLS fingerprinting).\nCannot access from Go — use the Teams web or desktop app to view assignments.")
}

func AssignmentStatusLabel(status string) string {
	switch status {
	case "submitted":
		return "[SUBMITTED]"
	case "returned":
		return "[RETURNED] "
	case "assigned":
		return "[PENDING]  "
	default:
		return "[ASSIGNMENT]"
	}
}
