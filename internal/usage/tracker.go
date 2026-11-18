package usage

import (
	"context"
	"time"

	"github.com/universal-api/platform/internal/database"
)

type UsageTracker interface {
	Record(ctx context.Context, keyID, capability, provider, requestID string, statusCode, durationMs int)
}

type Tracker struct {
	store database.Store
}

func NewTracker(store database.Store) *Tracker {
	return &Tracker{store: store}
}

func (t *Tracker) Record(ctx context.Context, keyID, capability, provider, requestID string, statusCode, durationMs int) {
	go func() {
		_ = t.store.RecordUsage(context.Background(), &database.UsageRecord{
			KeyID:      keyID,
			Capability: capability,
			Provider:   provider,
			StatusCode: statusCode,
			DurationMs: durationMs,
			RequestID:  requestID,
			CreatedAt:  time.Now().UTC(),
		})
	}()
}
