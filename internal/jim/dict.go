/* Jim - A small embeddable Tcl interpreter: the dictionary object.
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

import "fmt"

// Operations of dictHashFind. A value greater than zero means "update the
// table value offset of the existing entry to this value".
const (
	dictHashFind   = -1
	dictHashRemove = -2
	dictHashAdd    = -3
)

/* ---------------------------------------------------------------------------
 * Hashing
 * ------------------------------------------------------------------------ */

// genHashFunction is Jim's generic string hash; it walks the string backwards.
func genHashFunction(s []byte) uint32 {
	var result uint32
	for n := len(s) - 1; n >= 0; n-- {
		result += (result << 3) + uint32(s[n])
	}
	return result
}

// objectHTHashFunction hashes an object by its string representation. An
// integer with no string representation is hashed digit by digit, which gives
// exactly the same result as hashing the string would.
func objectHTHashFunction(key *Obj) uint32 {
	if key.kind == kindInt && !key.strValid {
		objValue := key.wide
		if objValue > minInt32 && objValue < maxInt32 {
			var result uint32
			value := uint32(objValue)
			if objValue < 0 {
				// wrap to positive (remove sign)
				value = uint32(-objValue)
			}
			// important: use a do-while, because value could be 0
			for {
				result += (result << 3) + uint32(value%10+'0')
				value /= 10
				if value == 0 {
					break
				}
			}
			if objValue < 0 {
				// negative, sign as char
				result += (result << 3) + '-'
			}
			return result
		}
	}
	return genHashFunction(key.Bytes())
}

const (
	minInt32 = -2147483648
	maxInt32 = 2147483647
	// htInitialSize is the smallest hash table Jim allocates.
	htInitialSize = 16
)

// hashTableNextPower rounds size up to a power of two of at least 16.
func hashTableNextPower(size uint32) uint32 {
	i := uint32(htInitialSize)
	if size >= 2147483648 {
		return 2147483648
	}
	for {
		if i >= size {
			return i
		}
		i *= 2
	}
}

/* ---------------------------------------------------------------------------
 * The Jim_Dict hash table
 * ------------------------------------------------------------------------ */

// dictHashFind searches the hash table for key and performs op, which is one
// of dictHashFind, dictHashRemove, dictHashAdd or a table value offset to
// store for an entry that must exist. It returns the table value offset of the
// entry, or 0 when the key was not found.
func (i *Interp) dictHashFind(d *Dict, key *Obj, op int) int {
	h := objectHTHashFunction(key) + d.uniq
	idx := h & d.sizeMask
	tvoffset := 0
	peturb := h
	const notRemoved = ^uint32(0)
	firstRemoved := notRemoved

	if len(d.table) != 0 && len(d.ht) != 0 {
		for {
			tvoffset = d.ht[idx].offset
			if tvoffset == 0 {
				break
			}
			if tvoffset == -1 {
				// An entry with offset -1 is a removed entry. We need to keep
				// going in case there is a non-removed entry later, but for
				// adds we prefer the first available removed entry.
				if firstRemoved == notRemoved {
					firstRemoved = idx
				}
			} else if d.ht[idx].hash == h && i.StringEqObj(key, d.table[tvoffset-1]) {
				break
			}
			// Use the Python algorithm for conflict resolution
			peturb >>= 5
			idx = (5*idx + 1 + peturb) & d.sizeMask
		}
	}

	switch {
	case op == dictHashFind:
		// If found return tvoffset, if not found return 0
	case op == dictHashRemove:
		if tvoffset != 0 {
			// Found, remove with -1 meaning a removed entry
			d.ht[idx].offset = -1
			d.dummy++
		}
	case op == dictHashAdd:
		if tvoffset == 0 {
			// Not found, so add it at the first removed entry, or the end
			if firstRemoved != notRemoved {
				idx = firstRemoved
				d.dummy--
			}
			d.ht[idx].offset = len(d.table) + 1
			d.ht[idx].hash = h
		}
	case tvoffset != 0:
		// Found, so replace the table value offset
		d.ht[idx].offset = op
	}

	return tvoffset
}

// expandHashTable allocates a hash table of at least the given size and
// reinserts the existing entries, which keeps their hashes.
func (d *Dict) expandHashTable(size uint32) {
	prevht := d.ht

	d.ht = make([]dictHashEntry, hashTableNextPower(size))
	d.sizeMask = uint32(len(d.ht)) - 1

	for _, e := range prevht {
		if e.offset > 0 {
			// Find the location in the new table for this entry
			idx := e.hash & d.sizeMask
			peturb := e.hash
			for d.ht[idx].offset != 0 {
				peturb >>= 5
				idx = (5*idx + 1 + peturb) & d.sizeMask
			}
			d.ht[idx] = dictHashEntry{offset: e.offset, hash: e.hash}
		}
	}
}

// dictAdd adds an entry for key, growing the hash table first if it is getting
// full. It returns the table value offset of an already existing entry, or 0
// after inserting a new entry with offset len(table) + 1.
func (i *Interp) dictAdd(d *Dict, key *Obj) int {
	// Note that the table holds both keys and values, so a dict with 4 keys
	// needs at least 8 hash entries, and dummy entries take up space too.
	if len(d.ht) <= len(d.table)+d.dummy {
		// The first add grows the size to 8, and thereafter it is doubled.
		if len(d.ht) != 0 {
			d.expandHashTable(uint32(len(d.ht)) * 2)
		} else {
			d.expandHashTable(8)
		}
	}
	return i.dictHashFind(d, key, dictHashAdd)
}

// dictNew returns a new dictionary with room for tableSize key/value entries
// and a hash table of at least htSize entries. Both may be 0.
func dictNew(tableSize, htSize int) *Dict {
	d := &Dict{}
	if htSize != 0 {
		d.expandHashTable(uint32(htSize))
	}
	if tableSize != 0 {
		d.table = make([]*Obj, 0, tableSize)
	}
	return d
}

// dup copies a dictionary. As in the C, the count of removed entries is not
// copied, so the copy may grow its hash table at a different point.
func (d *Dict) dup() *Dict {
	n := dictNew(cap(d.table), len(d.ht))
	n.table = append(n.table, d.table...)
	n.uniq = d.uniq
	// Copy the hash table directly, which is valid because the hashes and the
	// table offsets are unchanged.
	n.ht = make([]dictHashEntry, len(d.ht))
	copy(n.ht, d.ht)
	n.sizeMask = d.sizeMask
	return n
}

// setDictFromAny converts an object to the dictionary representation.
func (i *Interp) setDictFromAny(o *Obj) int {
	if o.kind == kindDict {
		return OK
	}

	if o.kind == kindList && !o.strValid {
		// The C only does this for a shared list, to avoid losing duplicate
		// keys from the string representation. Objects are shared values here,
		// so always keep the string representation of the list.
		o.Bytes()
	}

	// Convert a non-list object to a list and then to a dict, since we need
	// the list of key, value pairs anyway.
	listlen := i.ListLength(o)
	if listlen%2 != 0 {
		i.SetResultString("missing value to go with key")
		return ERR
	}

	// Allocate space in the hash table for twice the number of elements
	d := dictNew(0, listlen)

	// Take ownership of the list array
	table := o.list
	d.table = table[:0]

	// Now add all the elements to the hash table
	for n := 0; n < listlen; n += 2 {
		tvoffset := i.dictAdd(d, table[n])
		if tvoffset != 0 {
			// A duplicate key, so replace the value and don't add a new entry
			d.table[tvoffset] = table[n+1]
		} else {
			// Appending moves later entries down to fill any hole created by
			// a previous duplicate entry.
			d.table = append(d.table, table[n], table[n+1])
		}
	}

	o.freeIntRep()
	o.kind = kindDict
	o.dict = d
	return OK
}

/* ---------------------------------------------------------------------------
 * The dictionary API
 * ------------------------------------------------------------------------ */

// dictAddElement adds, replaces or (when val is nil) removes an entry of a
// dictionary object the caller owns. It returns ERR when a key to be removed
// does not exist.
func (i *Interp) dictAddElement(o *Obj, key, val *Obj) int {
	d := o.dict
	if val == nil {
		tvoffset := i.dictHashFind(d, key, dictHashRemove)
		if tvoffset == 0 {
			return ERR
		}
		// Found, so remove the pair from the table too; if it is not the last
		// pair, swap the last pair into the hole it leaves.
		table := d.table
		newLen := len(table) - 2
		if tvoffset != newLen+1 {
			table[tvoffset-1] = table[newLen]
			table[tvoffset] = table[newLen+1]
			// Now update the hash table for the swapped entry. The table is
			// shortened only afterwards because the search below still looks
			// at the entry that has just been moved away, as the C does.
			i.dictHashFind(d, table[tvoffset-1], tvoffset)
		}
		d.table = table[:newLen]
		return OK
	}

	tvoffset := i.dictAdd(d, key)
	if tvoffset != 0 {
		// Already exists, so just replace the value entry in the table
		d.table[tvoffset] = val
	} else {
		d.table = append(d.table, key, val)
	}
	return OK
}

// DictAddElement adds or replaces an entry of a dictionary object the caller
// owns. A nil value removes the key, which fails if it does not exist.
func (i *Interp) DictAddElement(d, key, val *Obj) int {
	if i.setDictFromAny(d) != OK {
		return ERR
	}
	d.invalidateStringRep()
	return i.dictAddElement(d, key, val)
}

// NewDictObj returns a new dictionary object from alternating key and value
// objects. It returns nil with an error result if the count is odd.
func (i *Interp) NewDictObj(elems []*Obj) *Obj {
	if len(elems)%2 != 0 {
		i.SetResultString("missing value to go with key")
		return nil
	}
	o := &Obj{charLen: -1, kind: kindDict, dict: dictNew(len(elems), len(elems))}
	for n := 0; n < len(elems); n += 2 {
		i.dictAddElement(o, elems[n], elems[n+1])
	}
	return o
}

// DictKey returns the value of one key of a dictionary. It returns ERR when
// the key is not present and -1 when the object is not a valid dictionary.
func (i *Interp) DictKey(d, key *Obj, flags int) (*Obj, int) {
	if i.setDictFromAny(d) != OK {
		return nil, -1
	}
	dict := d.dict
	tvoffset := i.dictHashFind(dict, key, dictHashFind)
	if tvoffset == 0 {
		if flags&ErrMsg != 0 {
			i.Errorf("key \"%#s\" not known in dictionary", key)
		}
		return nil, ERR
	}
	return dict.table[tvoffset], OK
}

// DictPairs returns the key/value table of a dictionary, which is only valid
// until the dictionary is next modified. It returns a nil slice and a length
// of 1 when the object cannot be converted to a dictionary.
func (i *Interp) DictPairs(d *Obj) ([]*Obj, int) {
	// If it is a list with an even number of elements, no need to convert to
	// a dict first, which would lose duplicate keys.
	if d.IsList() {
		table := i.ListElements(d)
		if len(table)%2 == 0 {
			return table, len(table)
		}
		// Otherwise fall through to get the standard error
	}
	if i.setDictFromAny(d) != OK {
		// Make sure the caller can tell an empty dict from a bad length
		return nil, 1
	}
	return d.dict.table, len(d.dict.table)
}

// DictKeysVector returns the value reached by following keys through nested
// dictionaries.
func (i *Interp) DictKeysVector(d *Obj, keys []*Obj, flags int) (*Obj, int) {
	if len(keys) == 0 {
		return d, OK
	}
	for _, key := range keys {
		o, rc := i.DictKey(d, key, flags)
		if rc != OK {
			return nil, rc
		}
		d = o
	}
	return d, OK
}

// SetDictKeysVector modifies the dictionary held in the variable varName,
// setting the element addressed by keys to newVal. A nil newVal removes the
// key; with MustExist a missing key is then an error. Unless NoResult is given
// the updated dictionary becomes the interpreter result.
func (i *Interp) SetDictKeysVector(varName *Obj, keys []*Obj, newVal *Obj, flags int) int {
	o := i.GetVariable(varName, flags)
	if o == nil {
		if newVal == nil && flags&MustExist != 0 {
			// Cannot remove a key from a non existing variable
			return ERR
		}
		o = i.NewDictObj(nil)
		if i.SetVariable(varName, o) != OK {
			return ERR
		}
	}
	// Objects are values here, so always modify a private copy, which is what
	// the C does for a shared object.
	varObj := o.dup()
	o = varObj
	for n := 0; n < len(keys); n++ {
		dictObj := o

		// Check if it's a valid dictionary
		if i.setDictFromAny(dictObj) != OK {
			return ERR
		}

		if n == len(keys)-1 {
			// Last key: note that an error on unset with a missing last key
			// is OK unless MustExist was given.
			if i.DictAddElement(o, keys[n], newVal) != OK {
				if newVal != nil || flags&MustExist != 0 {
					return ERR
				}
			}
			break
		}

		// Check if the given key exists
		dictObj.invalidateStringRep()
		keyFlags := ErrMsg
		if newVal != nil {
			keyFlags = None
		}
		sub, rc := i.DictKey(dictObj, keys[n], keyFlags)
		if rc == OK {
			// This key exists at the current level; make sure it's not shared
			o = sub.dup()
			i.dictAddElement(dictObj, keys[n], o)
		} else {
			// Key not found. For an [unset] operation this is an error; only
			// the last key may be missing.
			if newVal == nil {
				return ERR
			}
			// Otherwise set an empty dictionary as the key's value
			o = i.NewDictObj(nil)
			i.dictAddElement(dictObj, keys[n], o)
		}
	}
	o.invalidateStringRep()
	varObj.invalidateStringRep()
	if i.SetVariable(varName, varObj) != OK {
		return ERR
	}

	if flags&NoResult == 0 {
		i.SetResult(varObj)
	}
	return OK
}

// dictMatchTypes sets the interpreter result to a list of the keys, the values
// or both of a dictionary, optionally restricted to those whose key or value
// matches a glob pattern. matchType and returnTypes are DictMatchKeys and
// DictMatchValues flags.
func (i *Interp) dictMatchTypes(o *Obj, pattern *Obj, matchType, returnTypes int) int {
	if i.setDictFromAny(o) != OK {
		return ERR
	}
	table := o.dict.table

	listObj := i.NewListObj(nil)
	for n := 0; n < len(table); n += 2 {
		keyObj, valObj := table[n], table[n+1]
		if pattern != nil {
			matchObj := valObj
			if matchType == DictMatchKeys {
				matchObj = keyObj
			}
			if !i.StringMatchObj(pattern, matchObj, false) {
				continue
			}
		}
		if returnTypes&DictMatchKeys != 0 {
			i.ListAppendElement(listObj, keyObj)
		}
		if returnTypes&DictMatchValues != 0 {
			i.ListAppendElement(listObj, valObj)
		}
	}

	i.SetResult(listObj)
	return OK
}

// DictSize returns the number of key/value pairs of a dictionary, or -1 and
// ERR when the object is not a valid dictionary.
func (i *Interp) DictSize(d *Obj) (int, int) {
	if i.setDictFromAny(d) != OK {
		return -1, ERR
	}
	return len(d.dict.table) / 2, OK
}

// dictMerge returns a new dictionary with the entries of every argument, later
// arguments overriding earlier ones. It must be called with at least one.
func (i *Interp) dictMerge(argv []*Obj) *Obj {
	o := i.NewDictObj(nil)

	// Note that the trivial case of a single argument is not optimised
	for _, a := range argv {
		// If the object is a list, avoid converting it to a dictionary as we
		// may mishandle duplicate keys
		table, tablelen := i.DictPairs(a)
		if tablelen != 0 && table == nil {
			return nil
		}
		for n := 0; n < len(table); n += 2 {
			i.dictAddElement(o, table[n], table[n+1])
		}
	}
	return o
}

// dictInfo sets the interpreter result to a description of the hash table of a
// dictionary, using internal knowledge of its layout.
func (i *Interp) dictInfo(o *Obj) int {
	if i.setDictFromAny(o) != OK {
		return ERR
	}
	d := o.dict
	i.SetResultString(fmt.Sprintf("%d entries in table, %d buckets", len(d.table), len(d.ht)))
	return OK
}
