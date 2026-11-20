package cron

import "context"

// Cron file publication and retirement must not overtake one another inside
// the panel. The database/registration checks still enforce authority; this
// reservation only serializes the read/compare/write sequence.
var mutationToken = make(chan struct{}, 1)

func reserveMutation(ctx context.Context) (func(), error) {
	select {
	case mutationToken <- struct{}{}:
		return func() { <-mutationToken }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
