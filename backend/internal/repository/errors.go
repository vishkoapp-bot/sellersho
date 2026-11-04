package repository

import "errors"

// ErrInsufficientInventory is returned when a payment attempt cannot reserve
// or consume all requested product quantities.
var ErrInsufficientInventory = errors.New("insufficient inventory")
