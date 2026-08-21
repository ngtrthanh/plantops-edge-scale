package cycle

import (
	"context"
	"errors"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

func (c *Coordinator) ResolveCalled(ctx context.Context, plate, rfid string) (domain.WeighCycle, bool, error) {
	if c.store == nil {
		return domain.WeighCycle{}, false, errors.New("cycle store unavailable")
	}
	return c.store.FindCalledCycle(ctx, plate, rfid)
}

func (c *Coordinator) PairPolicy() domain.PairPolicy { return c.policy }
