package ui

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/helpers"
	tea "github.com/charmbracelet/bubbletea"
)

// Model values are copied by Bubble Tea. The owner is shared with the initial
// model so its deferred Close also covers terminal errors and external signals.
type renewalOwner struct {
	mu      sync.Mutex
	closed  bool
	process *helpers.AuthProcess
}

func (o *renewalOwner) start(cmd *exec.Cmd, timeout time.Duration) (*helpers.AuthProcess, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, context.Canceled
	}
	process, err := helpers.StartAuthProcess(cmd, timeout)
	if err == nil {
		o.process = process
	}
	return process, err
}

// Close stops any authentication helper before the terminal application exits.
// It is safe to call on the initial model, on updated copies, or more than once.
func (m Model) Close() {
	if m.renewal == nil {
		return
	}
	m.renewal.mu.Lock()
	m.renewal.closed = true
	process := m.renewal.process
	m.renewal.mu.Unlock()
	if process != nil {
		process.Close()
	}
}

const renewalLeadTime = 5 * time.Minute

type renewalPlan struct {
	provider   string
	generation uint64
	expiresAt  time.Time
}

func credentialForProvider(set *auth.CredentialSet, provider string) auth.Credential {
	switch provider {
	case CapabilityGraph:
		return set.Graph
	case CapabilityWeb:
		return set.Web
	case CapabilityNotif:
		return set.Notif
	case CapabilityEDU:
		return set.EDU
	default:
		return auth.Credential{}
	}
}

func renewalAttemptKey(provider string, generation uint64) string {
	return fmt.Sprintf("%s:%d", provider, generation)
}

func nextRenewalPlan(set *auth.CredentialSet, attempted map[string]bool) (renewalPlan, bool) {
	if set == nil {
		return renewalPlan{}, false
	}
	var plan renewalPlan
	for _, provider := range []string{CapabilityGraph, CapabilityWeb, CapabilityNotif, CapabilityEDU} {
		credential := credentialForProvider(set, provider)
		if credential.Token == "" || credential.ExpiresAt.IsZero() || attempted[renewalAttemptKey(provider, set.Generation)] {
			continue
		}
		candidate := renewalPlan{provider: provider, generation: set.Generation, expiresAt: credential.ExpiresAt}
		if !plan.expiresAt.IsZero() && !candidate.expiresAt.Before(plan.expiresAt) {
			continue
		}
		plan = candidate
	}
	return plan, !plan.expiresAt.IsZero()
}

func renewalTimerCmd(plan renewalPlan, now time.Time) tea.Cmd {
	delay := plan.expiresAt.Add(-renewalLeadTime).Sub(now)
	if delay < 0 {
		delay = 0
	}
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return renewalTimerMsg{provider: plan.provider, generation: plan.generation, expiresAt: plan.expiresAt}
	})
}

// credentialGeneration tolerates a model without a client (zero generation).
func (m *Model) credentialGeneration() uint64 {
	if m.client == nil {
		return 0
	}
	return m.client.Credentials().Generation
}

func scheduleNextRenewal(m *Model, now time.Time) tea.Cmd {
	if m.client == nil {
		return nil
	}
	attempted := m.renewalAttempted
	if attempted == nil {
		attempted = make(map[string]bool)
	}
	plan, ok := nextRenewalPlan(m.client.Credentials(), attempted)
	if !ok {
		return nil
	}
	return renewalTimerCmd(plan, now)
}

func (m *Model) timerMatches(plan renewalTimerMsg) bool {
	set := m.client.Credentials()
	if set.Generation != plan.generation {
		return false
	}
	credential := credentialForProvider(set, plan.provider)
	return credential.ExpiresAt.Equal(plan.expiresAt) && credential.Token != ""
}
