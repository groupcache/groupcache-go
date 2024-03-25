/*
Copyright Derrick J Wippler

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package transport

import (
	"errors"
)

// ErrNotFound should be returned from an implementation of `GetterFunc` to indicate the
// requested value is not available. When remote HTTP calls are made to retrieve values from
// other groupcache instances, returning this error will indicate to groupcache that the
// value requested is not available, and it should NOT attempt to call `GetterFunc` locally.
type ErrNotFound struct {
	Msg string
}

func (e *ErrNotFound) Error() string {
	return e.Msg
}

func (e *ErrNotFound) Is(target error) bool {
	var errNotFound *ErrNotFound
	ok := errors.As(target, &errNotFound)
	return ok
}

// ErrRemoteCall is returned from `group.Get()` when an error that is not a `ErrNotFound`
// is returned during a remote HTTP instance call
type ErrRemoteCall struct {
	Msg string
}

func (e *ErrRemoteCall) Error() string {
	return e.Msg
}

func (e *ErrRemoteCall) Is(target error) bool {
	var errRemoteCall *ErrRemoteCall
	ok := errors.As(target, &errRemoteCall)
	return ok
}
