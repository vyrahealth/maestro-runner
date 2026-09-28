package wda

import (
	"errors"
	"fmt"
)

// notFoundError is a lookup that read the screen and found nothing matching
// the selector. Any other lookup error means the screen could not be read
// (a failed /source, a hierarchy that would not parse), which says nothing
// about whether the element is there.
type notFoundError struct{ msg string }

func (e notFoundError) Error() string { return e.msg }

// notFound returns a notFoundError with a formatted message.
func notFound(format string, args ...interface{}) error {
	return notFoundError{msg: fmt.Sprintf(format, args...)}
}

// isNotFound reports whether err is a lookup that read the screen and found
// no match. Only that result means an element is gone.
func isNotFound(err error) bool {
	var nf notFoundError
	return errors.As(err, &nf)
}
