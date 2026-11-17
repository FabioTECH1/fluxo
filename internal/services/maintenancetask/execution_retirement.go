package maintenancetask

import (
	"context"
	"errors"

	"fluxo/internal/services/executiondomain"
)

func retireEmptyTaskUnit(ctx context.Context, v invocation, p map[string]string) error {
	if v.Status != "completed" && v.Status != "blocked" && v.Status != "interrupted" {
		return errors.New("system-task launch authority must be terminal before unit retirement")
	}
	if !executiondomain.UnitQuiescent(v.Adapter, p) {
		return errors.New("system-task unit metadata is not quiescent")
	}
	return executiondomain.StopNaturallyEmpty(ctx, v.Adapter, v.Unit, func() error {
		n, err := domainCount(v.Unit)
		if err != nil {
			return err
		}
		if n != 0 {
			return errors.New("system-task descendants must finish naturally")
		}
		return nil
	})
}
