package share

import (
	"context"
	"errors"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/celestiaorg/celestia-app/pkg/da"
)

// ErrNotAvailable is returned whenever DA sampling fails.
var ErrNotAvailable = errors.New("share: data not available")

// AvailabilityTimeout specifies timeout for DA validation during which data have to be found on
// the network, otherwise ErrNotAvailable is fired.
// TODO: https://github.com/celestiaorg/celestia-node/issues/10
const AvailabilityTimeout = 20 * time.Minute

// Root represents root commitment to multiple Shares.
// In practice, it is a commitment to all the Data in a square.
type Root = da.DataAvailabilityHeader

// Availability defines interface for validation of Shares' availability.
//
//go:generate mockgen -destination=availability/mocks/availability.go -package=mocks . Availability
type Availability interface {
	// SharesAvailable subjectively validates if Shares committed to the given Root are available on
	// the Network by requesting the EDS from the provided peers.
	SharesAvailable(context.Context, *Root, ...peer.ID) error
	// ProbabilityOfAvailability calculates the probability of the data square
	// being available based on the number of samples collected.
	// TODO(@Wondertan): Merge with SharesAvailable method, eventually
	ProbabilityOfAvailability(context.Context) float64
}
