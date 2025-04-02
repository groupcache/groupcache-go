package groupcache

// Info defines optional user-supplied per-request context fields that are
// propagated to the peer getter load function.
type Info struct {
	Ctx1 string
	Ctx2 string
}
