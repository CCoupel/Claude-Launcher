package platform

import "errors"

// ErrUnsupported : fonction non disponible sur ce système.
var ErrUnsupported = errors.New("non supporté sur ce système")
