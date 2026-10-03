package store

import (
	"context"
	"errors"

	"openusenet/internal/moderate"
)

func (m *Memory) ListModeratorRules(_ context.Context) ([]ModeratorRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ModeratorRule, len(m.modRules))
	copy(out, m.modRules)
	return out, nil
}

func (m *Memory) AddModeratorRule(_ context.Context, pattern, address string) error {
	pattern, address, err := moderate.NormalizeRule(pattern, address)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.modRules {
		if r.Pattern == pattern {
			m.modRules[i].Address = address
			return nil
		}
	}
	m.modRules = append(m.modRules, ModeratorRule{Pattern: pattern, Address: address})
	return nil
}

func (m *Memory) DeleteModeratorRule(_ context.Context, pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.modRules {
		if r.Pattern == pattern {
			m.modRules = append(m.modRules[:i], m.modRules[i+1:]...)
			return nil
		}
	}
	return errors.New("moderator rule not found")
}
