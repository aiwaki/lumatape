package app

import (
	"errors"
	"fmt"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/control"
	"github.com/aiwaki/lumatape/internal/locale"
)

// emergencyBarrier belongs to the main thread. Requests prepared before a
// stop cannot restore their old preferences, even if read after queue draining.
type emergencyBarrier struct{ sequence uint64 }

func (b *emergencyBarrier) advance() { b.sequence++ }
func (b *emergencyBarrier) check(expected *uint64) error {
	if expected == nil || *expected != b.sequence {
		return errors.New(locale.Text("настройки подготовлены до аварийного отключения или без его актуального счётчика; обновите состояние и повторите явное применение", "settings were prepared before emergency disable or without its current sequence; refresh state and explicitly apply again"))
	}
	return nil
}

// checkApply runs on the same locked main thread as ApplyDraft, before source
// resolution, OS mutation or save. Another preference command cannot run
// between this check and the commit. The emergency barrier takes priority even
// if the config happens to match.
func (b *emergencyBarrier) checkApply(p control.ApplyPayload, current config.Config) *control.Failure {
	if err := b.check(p.ExpectedEmergencySequence); err != nil {
		return &control.Failure{Code: "stale_emergency_sequence", Message: err.Error()}
	}
	if len(p.ExpectedConfig) == 0 {
		return nil // Existing clients did not provide a config precondition.
	}
	expected, err := config.Decode(p.ExpectedConfig)
	if err != nil {
		return &control.Failure{Code: "invalid_payload", Message: fmt.Sprintf("expected_config: %v", err)}
	}
	if expected != current {
		return &control.Failure{Code: "stale_config", Message: locale.Text("настройки изменились после выбора команды; обновите состояние и повторите действие", "settings changed after the command was selected; refresh state and try again")}
	}
	return nil
}

func needsSourceResolution(plan transitionPlan, explicitSource bool) bool {
	return explicitSource || plan.Source || plan.Capture || plan.ApplyWindow || plan.ApplySystem
}
