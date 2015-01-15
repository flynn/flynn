package volume

import (
	"encoding/json"
)

/*
	Provider defines the interface of a, well, a provider of volumes.
	Providers may have different backing implementations (i.e zfs vs btrfs vs etc).
	Provider instances may also vary in their parameters, so for example the volume.Manager on a host
	could be configured with two different Providers that both use zfs, but have different zpools
	configured for their final storage location.
*/
type Provider interface {
	NewVolume() (Volume, error)
}

type ProviderSpec struct {
	// ID used by the API to specify this provider
	ID string `json:"id"`

	// names the kind of provider (roughly matches an enum of known names)
	Kind string `json:"kind"`

	// parameters to pass to the provider during its creation/configuration.
	// values vary per implementation kind;
	// see the ProviderConfig struct in implementation packages for known values.
	Config json.RawMessage `json:"metadata,omitempty"`
}
