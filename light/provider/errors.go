package provider

import (
	"errors"
	"fmt"
)

var (
	// ErrLightBlockNotFound is returned when a provider can't find the
	// requested header.
	ErrLightBlockNotFound = errors.New("light block not found")
	// ErrNoResponse is returned if the provider doesn't respond to the
	// request in a gieven time
	ErrNoResponse = errors.New("client failed to respond")
)

// ErrBadLightBlock is returned when a provider returns an invalid
// light block.
type ErrBadLightBlock struct {
	Reason error
}

func (e ErrBadLightBlock) Error() string {
	return fmt.Sprintf("client provided bad signed header: %w", e.Reason)
}