//go:build !linux

package networking

import (
	"errors"
)

func NewDataPlane(DataPlaneOptions) (DataPlane, error) {
	return nil, errors.New("cellular data plane is supported only on Linux")
}
