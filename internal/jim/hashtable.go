/* Jim - A small embeddable Tcl interpreter: the hash table of commands and variables.
 *
 * Copyright 2005 Salvatore Sanfilippo <antirez@invece.org>
 * Copyright 2005 Clemens Hintze <c.hintze@gmx.net>
 * Copyright 2008 Steve Bennett <steveb@workware.net.au>
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 *
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above
 *    copyright notice, this list of conditions and the following
 *    disclaimer in the documentation and/or other materials
 *    provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE JIM TCL PROJECT ``AS IS'' AND ANY
 * EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO,
 * THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A
 * PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE
 * JIM TCL PROJECT OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT,
 * INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
 * (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT,
 * STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF
 * ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 * The views and conclusions contained in the software and documentation
 * are those of the authors and should not be interpreted as representing
 * official policies, either expressed or implied, of the Jim Tcl Project.
 */

package jim

// The order in which the interpreter's commands and variables are listed by
// [info commands], [info procs], [info globals], [info locals] and [info vars]
// is the order of this hash table, so it is ported exactly: the same hash
// function, the same power-of-two table sizes, insertion at the head of a
// bucket's chain, growth when the table is as full as its size, and iteration
// from the first bucket to the last along each chain.

// cmdTableInitialSize is JIM_HT_INITIAL_SIZE, the smallest command/variable table.
const cmdTableInitialSize = 16

// genHash is Jim_GenHashFunction: the bytes are mixed in from the last to the
// first.
func genHash(key string) uint32 {
	var result uint32
	for i := len(key) - 1; i >= 0; i-- {
		result += (result << 3) + uint32(key[i])
	}
	return result
}

// stripNamespace drops a leading :: from a command name, as
// Jim_GetStringNoQualifier does, so that ::foo and foo are the same key.
func stripNamespace(key string) string {
	if len(key) >= 2 && key[0] == ':' && key[1] == ':' {
		for len(key) > 0 && key[0] == ':' {
			key = key[1:]
		}
	}
	return key
}

type htEntry[V any] struct {
	key  string
	val  V
	next *htEntry[V]
}

// htable is a Jim_HashTable with string keys.
type htable[V any] struct {
	buckets  []*htEntry[V]
	sizeMask uint32
	used     int
	// qualified is set for the command table, whose keys ignore a leading ::
	qualified bool
}

func (h *htable[V]) norm(key string) string {
	if h.qualified {
		return stripNamespace(key)
	}
	return key
}

func (h *htable[V]) index(key string) uint32 {
	return genHash(h.norm(key)) & h.sizeMask
}

// expand grows the table to at least size entries, keeping the chain order the
// C implementation produces.
func (h *htable[V]) expand(size int) {
	realsize := cmdTableInitialSize
	for realsize < size {
		realsize *= 2
	}
	if size <= h.used {
		return
	}
	old := h.buckets
	h.buckets = make([]*htEntry[V], realsize)
	h.sizeMask = uint32(realsize - 1)
	for _, he := range old {
		for he != nil {
			next := he.next
			idx := h.index(he.key)
			he.next = h.buckets[idx]
			h.buckets[idx] = he
			he = next
		}
	}
}

func (h *htable[V]) expandIfNeeded() {
	if len(h.buckets) == 0 {
		h.expand(cmdTableInitialSize)
	}
	if h.used == len(h.buckets) {
		h.expand(len(h.buckets) * 2)
	}
}

// find returns the entry for key, or nil.
func (h *htable[V]) find(key string) *htEntry[V] {
	if h == nil || h.used == 0 {
		return nil
	}
	want := h.norm(key)
	for he := h.buckets[h.index(key)]; he != nil; he = he.next {
		if h.norm(he.key) == want {
			return he
		}
	}
	return nil
}

// get returns the value stored for key.
func (h *htable[V]) get(key string) (V, bool) {
	if he := h.find(key); he != nil {
		return he.val, true
	}
	var zero V
	return zero, false
}

// set stores a value, replacing an existing entry's value and keeping its
// place in the chain.
func (h *htable[V]) set(key string, val V) {
	if he := h.find(key); he != nil {
		he.val = val
		return
	}
	h.expandIfNeeded()
	idx := h.index(key)
	h.buckets[idx] = &htEntry[V]{key: key, val: val, next: h.buckets[idx]}
	h.used++
}

// delete removes an entry, reporting whether it was there.
func (h *htable[V]) delete(key string) bool {
	if h.used == 0 {
		return false
	}
	idx := h.index(key)
	want := h.norm(key)
	var prev *htEntry[V]
	for he := h.buckets[idx]; he != nil; he = he.next {
		if h.norm(he.key) == want {
			if prev != nil {
				prev.next = he.next
			} else {
				h.buckets[idx] = he.next
			}
			h.used--
			return true
		}
		prev = he
	}
	return false
}

// keys returns the keys in the table's own iteration order.
func (h *htable[V]) keys() []string {
	if h == nil {
		return nil
	}
	out := make([]string, 0, h.used)
	for _, he := range h.buckets {
		for ; he != nil; he = he.next {
			out = append(out, he.key)
		}
	}
	return out
}

// entries returns the key/value pairs in the table's iteration order. A nil
// table is empty, which is what the C's NULL static-variable table means.
func (h *htable[V]) entries() []*htEntry[V] {
	if h == nil {
		return nil
	}
	out := make([]*htEntry[V], 0, h.used)
	for _, he := range h.buckets {
		for ; he != nil; he = he.next {
			out = append(out, he)
		}
	}
	return out
}

func (h *htable[V]) len() int {
	if h == nil {
		return 0
	}
	return h.used
}

// clear empties the table for reuse.
func (h *htable[V]) clear() {
	h.buckets = nil
	h.sizeMask = 0
	h.used = 0
}
