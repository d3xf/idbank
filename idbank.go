/*

Package idbank provides a concurrency-safe service for allocating unique
temporary identifiers.

In environments with many concurrently-executing services or processes, it
is sometimes desirable to dispense globally unique identifiers on a
provisional basis.  An identifier is reclaimed either when explicitly
released by a client, or after an expiration period.  A client may reset
the expiration time for an identifier to keep it from expiring while it's
still in use.

This package implements such a dispenser for identifiers of type
uint32. (Other identifier types can be supported with minimal changes.)
The basic abstraction is a Bank, representing a store of identifiers
within a user-specified range that supports allocation, release, and
expiration-reset operations.  When an identifier is allocated, a random
token is returned that must be supplied when attempting a reset or
release, preventing accidental disruption by other clients.  All
operations are safe for concurrent use.

A small example HTTP server program alongside the package provides a
simple JSON REST API for interacting with an identifier microservice.

*/
package idbank

import (
	"errors"
	"log"
	"math/rand"
	"time"
)

// This package serves as an example of a basic and powerful pattern: We
// create a private object and a goroutine that mediates all access to the
// object, avoiding the pitfalls of shared-memory synchronization. The
// public API simply sends messages to the goroutine, which waits in a
// select loop for messages and processes them as they're received.

// Debug controls whether debugging messages for the package are logged.
const Debug = true

func debug(args ...interface{}) {
	if Debug {
		log.Println(args...)
	}
}

// ID is the type of identifier provided to clients.
type ID uint32

// This interface is the generic type used for messages sent to our
// control channel. Different messages are implemented as distinct types
// that satisfy this interface (with the trivial function).  See
// http://www.jerf.org/iri/post/2917
type controlMessage interface {
	isControlMessage()
}

// A Bank represents a store of unique identifiers within a specific range.
type Bank struct {
	bank    map[ID]bankEntry    // Identifier store
	freeptr ID                  // Head of the freelist
	max     ID                  // One more than the max permitted ID value
	control chan controlMessage // Main control channel
	rnd     *rand.Rand          // For generating random tokens
}

// A bankEntry represents the state of an identifier. If the identifier is
// allocated, then the timer field contains the active timer for this
// identifier.  If the identifier is free, then nextfree points to the
// next free identifier on the freelist.
type bankEntry struct {
	client   string      // Name of the client who allocated this ID
	token    int32       // Authentication token
	t        *time.Timer // Live expiration timer
	nextfree ID          // Next free identifier on freelist
}

// New creates and returns a new Bank with identifier range [min, max)
// (i.e., inclusive of min and exclusive of max).  If min >= max then the
// range [0, 2^32-1) will be used.
func New(min ID, max ID) *Bank {
	if min >= max {
		min, max = 0, 0xffffffff
	}
	b := &Bank{
		bank:    make(map[ID]bankEntry),
		freeptr: min,
		max:     max,
		control: make(chan controlMessage),
		rnd:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	// Start control goroutine. This routine mediates all access to
	// the Bank, ensuring operations are concurrency-safe. User API
	// functions just send messages here.
	go func() {
		for {
			m := <-b.control
			switch msg := m.(type) {
			case allocID:
				msg.response <- b.alloc(msg.client, msg.exptime)
			case queryID:
				msg.response <- b.query(msg.id)
			case releaseID:
				msg.response <- b.release(msg.id, msg.token)
			case resetID:
				msg.response <- b.reset(msg.id, msg.exptime, msg.token)
			case destroyBank:
				b.destroy()
				return
			default:
				panic("control: received unknown message")
			}
		}
	}()
	debug("new: new bank created")
	return b
}

// alloc allocates a new identifier from the Bank with a timeout specified
// by the client. If all identifiers are in use, a non-nil error is returned.
func (b *Bank) alloc(client string, exptime time.Duration) struct {
	uint64
	error
} {
	debug("alloc: called with exptime =", exptime)
	id := b.freeptr // New ID is the head of the freelist
	if _, ok := b.bank[id]; ok {
		// Slot for this ID is already on the freelist
		e := b.bank[id]
		b.freeptr = e.nextfree
		e.nextfree = 0
		b.bank[id] = e
	} else {
		// Slot for this ID doesn't exist yet
		if b.freeptr == b.max {
			debug("alloc: error: all identifiers in use")
			return struct {
				uint64
				error
			}{0, errors.New("all identifiers in use")}
		}
		b.freeptr += 1
		b.bank[id] = bankEntry{}
	}
	e := b.bank[id]
	e.client = client
	e.token = b.rnd.Int31()
	// Kick off expiration timer
	e.t = time.AfterFunc(exptime, func() {
		debug("expire: timeout expired for id", id)
		r := make(chan error)
		b.control <- releaseID{id, e.token, r}
		<-r
	})
	b.bank[id] = e
	debug("alloc: allocated id", id, "token", e.token)
	return struct {
		uint64
		error
	}{uint64(id)<<32 | uint64(e.token), nil}
}

// Alloc allocates and returns a new identifier with the specified client
// name and expiration timeout. A token is also returned that must be
// supplied when resetting or releasing this identifier. If all
// identifiers are in use, a non-nil error is returned.
func (b *Bank) Alloc(client string, exptime time.Duration) (ID, int32, error) {
	response := make(chan struct {
		uint64
		error
	})
	b.control <- allocID{client, exptime, response}
	r := <-response
	if r.error != nil {
		return 0, 0, r.error
	}
	return ID(r.uint64 >> 32), int32(r.uint64 & 0xffffffff), nil
}

func (b *Bank) isAllocated(id ID) bool {
	e, ok := b.bank[id]
	return ok && e.t != nil
}

// release deallocates the given ID if it's currently allocated, and puts
// it on the freelist.
func (b *Bank) release(id ID, token int32) error {
	debug("release: called with id", id, "token", token)
	if b.isAllocated(id) {
		e := b.bank[id]
		if token == e.token {
			e.t.Stop()
			e.t = nil
			e.token = -1
			e.nextfree = b.freeptr
			b.bank[id] = e

			b.freeptr = id
			debug("release: released id", id)
			return nil
		} else {
			debug("release: error: invalid token", token, "!=", e.token)
			return errors.New("invalid token")
		}
	}
	debug("release: error: unallocated id", id)
	return errors.New("unallocated id")
}

// Release releases the specified identifier back to the system. If ID is
// not allocated or the token doesn't match, a non-nil error is returned.
func (b *Bank) Release(id ID, token int32) error {
	response := make(chan error)
	b.control <- releaseID{id, token, response}
	return <-response
}

// reset resets the expiration timer for an ID.
func (b *Bank) reset(id ID, exptime time.Duration, token int32) error {
	debug("reset: called with id", id, "token", token)
	if b.isAllocated(id) {
		if token == b.bank[id].token {
			_ = b.bank[id].t.Reset(exptime)
			debug("reset: reset exptime of id", id, "to", exptime)
			return nil
		} else {
			debug("reset: error: invalid token", token, "!=", b.bank[id].token)
			return errors.New("invalid token")
		}
	}
	debug("reset: error: unallocated id", id)
	return errors.New("unallocated id")
}

// Reset resets the expiration timer for the specified identifier. If ID
// not allocated or the token doesn't match, a non-nil error is returned.
func (b *Bank) Reset(id ID, exptime time.Duration, token int32) error {
	response := make(chan error)
	b.control <- resetID{id, exptime, token, response}
	return <-response
}

// query returns the client name associated with ID.
func (b *Bank) query(id ID) string {
	debug("query: called with id", id)
	if b.isAllocated(id) {
		debug("query: returning", b.bank[id].client, "for id", id)
		return b.bank[id].client
	} else {
		debug("query: id", id, "not allocated, returning null")
		return ""
	}
}

// Query returns the client name associated with an identifier if it is
// allocated, and returns "" otherwise.
func (b *Bank) Query(id ID) string {
	response := make(chan string)
	b.control <- queryID{id, response}
	return <-response
}

// destroy clears all state associated with a Bank.
func (b *Bank) destroy() {
	b.bank = nil
	b.control = nil
	b.rnd = nil
	debug("destroy: bank destroyed")
}

// Destroy releases all resources associated with a Bank, after which it
// must not be used.
func (b *Bank) Destroy() {
	if b.bank != nil {
		b.control <- destroyBank{}
	}
}

// Control message types
type allocID struct {
	client   string
	exptime  time.Duration
	response chan struct {
		uint64
		error
	}
}

func (a allocID) isControlMessage() {}

type queryID struct {
	id       ID
	response chan string
}

func (q queryID) isControlMessage() {}

type releaseID struct {
	id       ID
	token    int32
	response chan error
}

func (r releaseID) isControlMessage() {}

type resetID struct {
	id       ID
	exptime  time.Duration
	token    int32
	response chan error
}

func (r resetID) isControlMessage() {}

type destroyBank struct {
}

func (d destroyBank) isControlMessage() {}
