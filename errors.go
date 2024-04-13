package groupcache

import (
	"strings"
)

type MultiError struct {
	errors []error
}

func (m *MultiError) Add(err error) {
	if err != nil {
		m.errors = append(m.errors, err)
	}
}

func (m *MultiError) Error() string {
	if len(m.errors) == 0 {
		return ""
	}

	var errStrings []string
	for _, err := range m.errors {
		errStrings = append(errStrings, err.Error())
	}
	return strings.Join(errStrings, "\n")
}

func (m *MultiError) NilOrError() error {
	if len(m.errors) == 0 {
		return nil
	}
	return m
}

// ErrRemoteCall is returned from `group.Get()` when a remote GetterFunc returns an
// error. When this happens `group.Get()` does not attempt to retrieve the value
// via our local GetterFunc.
type ErrRemoteCall struct {
	Msg string
}
