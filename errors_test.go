package groupcache

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMultiError(t *testing.T) {
	m := &MultiError{}
	err := m.NilOrError()
	assert.NoError(t, err)

	m.Add(errors.New("this one error"))
	err = m.NilOrError()
	assert.Error(t, err)
	assert.Equal(t, "this one error", err.Error())

	m.Add(errors.New("this a second error"))
	err = m.NilOrError()
	assert.Error(t, err)
	assert.Equal(t, "this one error\nthis a second error", err.Error())
}
