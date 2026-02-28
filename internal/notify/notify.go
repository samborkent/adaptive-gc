// Adapted from github.com/CAFxX/gcnotifier, slightly modernized.
//
// Package notify provides a way to receive notifications after every
// garbage collection (GC) cycle. This can be useful, in long-running programs,
// to instruct your code to free additional memory resources that you may be
// using.
//
// A common use case for this is when you have custom data structures (e.g.
// buffers, caches, rings, trees, pools, ...): instead of setting a maximum size
// to your data structure you can leave it unbounded and then drop all (or some)
// of the allocated-but-unused slots after every GC run (e.g. sync.Pool drops
// all allocated-but-unused objects in the pool during GC).
//
// To minimize the load on the GC the code that runs after receiving the
// notification should try to avoid allocations as much as possible, or at the
// very least make sure that the amount of new memory allocated is significantly
// smaller than the amount of memory that has been "freed" in response to the
// notification.
//
// Notifier guarantees to send a notification after every GC cycle completes.
// Note that the Go runtime does not guarantee that the GC will run:
// specifically there is no guarantee that a GC will run before the program
// terminates.
package notify

import "runtime"

// Notifier allows your code to control and receive notifications every time
// the garbage collector runs.
type Notifier struct {
	n *gcnotifier
}

type gcnotifier struct {
	doneCh chan struct{}
	gcCh   chan struct{}
}

type sentinel gcnotifier

// AfterGC returns the channel that will receive a notification after every GC
// run. No further notifications will be sent until the previous notification
// has been consumed. To stop notifications immediately call the Close() method.
// Otherwise notifications will continue until the Notifier object itself is
// garbage collected. Note that the channel returned by AfterGC will be closed
// only when Notifier is garbage collected.
// The channel is unique to a single Notifier object: use dedicated
// GCNotifiers if you need to listen for GC notifications in multiple receivers
// at the same time.
func (n *Notifier) AfterGC() <-chan struct{} {
	return n.n.gcCh
}

// Close will stop and release all resources associated with the Notifier. It
// is not required to call Close explicitly: when the Notifier object is
// garbage collected Close is called implicitly.
// If you don't call Close explicitly make sure not to accidently maintain the
// Notifier object alive.
func (n *Notifier) Close() {
	autoclose(n.n.doneCh)
}

// autoclose is both called explicitely via Close or when the Notifier is
// garbage collected
// func autoclose(n *gcnotifier) {
func autoclose(doneCh chan<- struct{}) {
	select {
	case doneCh <- struct{}{}:
	default:
	}
}

// New creates and arms a new Notifier.
func New() *Notifier {
	n := &gcnotifier{
		gcCh:   make(chan struct{}, 1),
		doneCh: make(chan struct{}, 1),
	}
	// sentinel is dead immediately after the call to SetFinalizer
	runtime.SetFinalizer(&sentinel{gcCh: n.gcCh, doneCh: n.doneCh}, finalizer)
	// n will be dead when the Notifier that wraps it (see the return below) is dead
	runtime.AddCleanup(n, autoclose, n.doneCh)
	// we wrap the internal gcnotifier object in a Notifier so that we can
	// safely call autoclose when the Notifier becomes unreachable
	return &Notifier{n: n}
}

func finalizer(s *sentinel) {
	// check if we have to shutdown
	select {
	case <-s.doneCh:
		close(s.gcCh)
		return
	default:
	}

	// send the notification
	select {
	case s.gcCh <- struct{}{}:
	default:
		// drop it if there's already an unread notification in gcCh
	}

	// rearm the finalizer
	runtime.SetFinalizer(s, finalizer)
}
