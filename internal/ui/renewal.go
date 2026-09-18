package ui

import (
	"time"

	"github.com/agmonetti/lazyteams/internal/auth"
	tea "github.com/charmbracelet/bubbletea"
)

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

func nextRenewalPlan(set *auth.CredentialSet, attempted map[string]uint64) (renewalPlan, bool) {
	if set == nil {
		return renewalPlan{}, false
	}
	var plan renewalPlan
	for _, provider := range []string{CapabilityGraph, CapabilityWeb, CapabilityNotif, CapabilityEDU} {
		credential := credentialForProvider(set, provider)
		if credential.Token == "" || credential.ExpiresAt.IsZero() || attempted[provider] == set.Generation {
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

func scheduleNextRenewal(m *Model, now time.Time) tea.Cmd {
	plan, ok := nextRenewalPlan(m.client.Credentials(), m.renewalAttemptGeneration)
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
