package fault

import (
	"context"
	"errors"
	"fmt"
)

type Category string

const (
	Invalid   Category = "invalid"
	NotFound  Category = "not_found"
	Conflict  Category = "conflict"
	Cancelled Category = "cancelled"
	Ambiguous Category = "ambiguous_outcome"
	Internal  Category = "internal"
)

type Error struct {
	Operation string
	Category  Category
	Retryable bool
	Ambiguous bool
	Err       error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Operation, e.Category, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func New(operation string, category Category, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		category = Cancelled
	}
	return &Error{Operation: operation, Category: category, Err: err}
}

func IsCategory(err error, category Category) bool {
	var target *Error
	return errors.As(err, &target) && target.Category == category
}

func CategoryOf(err error) Category {
	var target *Error
	if errors.As(err, &target) {
		return target.Category
	}
	return Internal
}
