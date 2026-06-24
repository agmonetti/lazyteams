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
		return nil, fmt.Errorf("EDU_TOKEN no configurado")
	}
	if c.EduCookie == "" {
		return nil, fmt.Errorf("EDU_COOKIE no configurado.\nCapturalo de DevTools > Network > assignments.edu.cloud.microsoft > header Cookie")
	}

	return nil, fmt.Errorf("API de Education Assignments bloqueada por WAF (TLS fingerprinting).\nDesde Go no se puede acceder — usá la app de Teams web o escritorio para ver tareas.")
}

func AssignmentStatusLabel(status string) string {
	switch status {
	case "submitted":
		return "[ENTREGADA]"
	case "returned":
		return "[DEVUELTA] "
	case "assigned":
		return "[PENDIENTE]"
	default:
		return "[TAREA]    "
	}
}
