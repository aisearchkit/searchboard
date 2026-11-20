package domain

import "errors"

var (
	// ErrNotFound is returned when a requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when an entity violates a uniqueness rule.
	ErrConflict = errors.New("already exists")
)
