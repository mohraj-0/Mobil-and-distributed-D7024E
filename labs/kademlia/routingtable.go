package kademlia

import (
	"crypto/rand"
	"sync"
	"time"
)

const bucketSize = 10
const defaultRoutingTreeBranchBits = 1

// RoutingTable stores known contacts in a Kademlia routing tree. Leaf nodes are
// k-buckets; full leaves split when they contain the local ID or depth%b != 0.
type RoutingTable struct {
	mu         sync.Mutex
	capacity   int
	me         Contact
	root       *routingTreeNode
	branchBits int
}

type routingTreeNode struct {
	// A node is a leaf when bucket is non-nil. Internal nodes set bucket to nil
	// and route contacts into children according to the next ID bit.
	bucket   *bucket
	children []*routingTreeNode
	updated  time.Time

	// depth is the number of ID prefix bits already consumed on the path from
	// the root to this node. Each binary split consumes one bit.
	depth int

	// Local buckets may always split. Non-local buckets may split at depth%b != 0.
	containsMe bool
}

// NewRoutingTable creates a routing tree with b = 1, which is the standard
// binary Kademlia tree and the simplified option allowed by the lab spec.
func NewRoutingTable(me Contact) *RoutingTable {
	return NewRoutingTableWithBranchBits(me, defaultRoutingTreeBranchBits)
}

// NewRoutingTableWithBranchBits creates a generalized routing tree where b is
// the acceleration parameter from the lab routing-tree supplement. Splits remain binary; b permits extra splits at depths not divisible by b.
func NewRoutingTableWithBranchBits(me Contact, branchBits int) *RoutingTable {
	if branchBits < 1 {
		branchBits = 1
	}
	if branchBits > IDLength*8 {
		branchBits = IDLength * 8
	}

	return &RoutingTable{
		capacity:   bucketSize,
		me:         me,
		root:       newRoutingTreeNode(0, true),
		branchBits: branchBits,
	}
}

func newRoutingTreeNode(depth int, containsMe bool) *routingTreeNode {
	return &routingTreeNode{
		bucket:     newBucket(),
		depth:      depth,
		containsMe: containsMe,
		updated:    time.Now(),
	}
}

// AddContact inserts a contact into the appropriate leaf bucket. If that bucket
// is full and may split according to b, it splits and insertion is retried.
func (routingTable *RoutingTable) AddContact(contact Contact) {
	routingTable.mu.Lock()
	defer routingTable.mu.Unlock()
	routingTable.add(contact)
}

func (routingTable *RoutingTable) add(contact Contact) {
	if contact.ID == nil {
		return
	}
	// A node never keeps itself in its routing table.
	if routingTable.me.ID != nil && contact.ID.Equals(routingTable.me.ID) {
		return
	}

	routingTable.addContact(routingTable.root, contact)
}

func (routingTable *RoutingTable) addContact(node *routingTreeNode, contact Contact) {
	if node.isLeaf() {
		// Existing contacts are allowed through even when the bucket is full so
		// bucket.AddContact can move them to the front as "recently seen".
		if node.bucket.Len() < routingTable.capacity || node.bucket.hasContact(contact) || !routingTable.canSplit(node) {
			node.bucket.AddContact(contact)
			if node.bucket.hasContact(contact) {
				node.updated = time.Now()
			}
			return
		}

		// Split an eligible full leaf and retry insertion in its child.
		routingTable.split(node)
		routingTable.addContact(node, contact)
		return
	}

	routingTable.addContact(routingTable.child(node, contact.ID), contact)
}

func (node *routingTreeNode) isLeaf() bool {
	return node.bucket != nil
}

func (rt *RoutingTable) canSplit(node *routingTreeNode) bool {
	return node.depth < IDLength*8 && (node.containsMe || node.depth%rt.branchBits != 0)
}

func (routingTable *RoutingTable) split(node *routingTreeNode) {
	bits := routingTable.bitsAtDepth(node.depth)
	childCount := 1 << bits
	children := make([]*routingTreeNode, childCount)
	meChildIndex := routingTable.childIndex(routingTable.me.ID, node.depth)

	for i := range children {
		children[i] = newRoutingTreeNode(node.depth+bits, node.containsMe && i == meChildIndex)
		children[i].bucket.capacity = routingTable.capacity
	}

	contacts := node.bucket.Contacts()
	node.bucket = nil
	node.children = children

	// Redistribute the old bucket contents using the same prefix bits that will
	// route future contacts into these children.
	for i := len(contacts) - 1; i >= 0; i-- {
		contact := contacts[i]
		childIndex := routingTable.childIndex(contact.ID, node.depth)
		children[childIndex].bucket.AddContact(contact)
	}
}

// FindClosestContacts returns the count contacts with smallest XOR distance to
// target. The tree is used for storage; the final result is sorted by distance.
func (routingTable *RoutingTable) FindClosestContacts(target *KademliaID, count int) []Contact {
	routingTable.mu.Lock()
	defer routingTable.mu.Unlock()
	if target == nil || count <= 0 {
		return nil
	}

	var candidates ContactCandidates
	candidates.Append(routingTable.contacts())
	for i := range candidates.contacts {
		candidates.contacts[i].CalcDistance(target)
	}
	candidates.Sort()

	return candidates.GetContacts(count)
}

// Me returns the local node contact that owns this routing table.
func (routingTable *RoutingTable) Me() Contact {
	return routingTable.me
}

// NonEmptyBucketIndices returns the indices of non-empty leaf buckets in a
// deterministic left-to-right tree traversal.
func (routingTable *RoutingTable) NonEmptyBucketIndices() []int {
	routingTable.mu.Lock()
	defer routingTable.mu.Unlock()
	return collectLeafValues(routingTable, func(index int, node *routingTreeNode, _ KademliaID) (int, bool) {
		if node.bucket.Len() == 0 {
			return 0, false
		}
		return index, true
	})
}

// ContactsInBucket returns the current contents of one leaf bucket.
func (routingTable *RoutingTable) ContactsInBucket(bucketIndex int) []Contact {
	routingTable.mu.Lock()
	defer routingTable.mu.Unlock()
	matches := collectLeafValues(routingTable, func(index int, node *routingTreeNode, prefix KademliaID) ([]Contact, bool) {
		if index != bucketIndex {
			return nil, false
		}
		// Distances are calculated against the bucket prefix so show/debug output
		// has deterministic ordering inside the printed bucket.
		return node.bucket.GetContactAndCalcDistance(&prefix), true
	})
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

// RefreshIDForBucket returns a random ID inside the selected leaf bucket range.
func (routingTable *RoutingTable) RefreshIDForBucket(bucketIndex int) *KademliaID {
	routingTable.mu.Lock()
	defer routingTable.mu.Unlock()
	matches := collectLeafValues(routingTable, func(index int, node *routingTreeNode, prefix KademliaID) (*KademliaID, bool) {
		if index != bucketIndex {
			return nil, false
		}
		target := prefix
		var random KademliaID
		if _, err := rand.Read(random[:]); err != nil {
			return nil, false
		}
		for bit := node.depth; bit < IDLength*8; bit++ {
			setBit(&target, bit, bitAt(&random, bit))
		}
		node.updated = time.Now()
		return &target, true
	})
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

// BranchBits returns the configured routing-tree branching parameter b.
func (routingTable *RoutingTable) BranchBits() int {
	return routingTable.branchBits
}

func (routingTable *RoutingTable) contacts() []Contact {
	contacts := make([]Contact, 0)
	for _, bucketContacts := range collectLeafValues(routingTable, func(_ int, node *routingTreeNode, _ KademliaID) ([]Contact, bool) {
		return node.bucket.Contacts(), true
	}) {
		contacts = append(contacts, bucketContacts...)
	}
	return contacts
}

// collectLeafValues walks leaf buckets once and lets callers choose the result
// type. The bool return acts like a filter, so callers do not need separate
// traversal code for indices, contacts, or refresh IDs.
func collectLeafValues[T any](routingTable *RoutingTable, pick func(int, *routingTreeNode, KademliaID) (T, bool)) []T {
	values := make([]T, 0)
	leafIndex := 0

	var walk func(*routingTreeNode, KademliaID)
	walk = func(node *routingTreeNode, prefix KademliaID) {
		if node.isLeaf() {
			value, ok := pick(leafIndex, node, prefix)
			leafIndex++
			if ok {
				values = append(values, value)
			}
			return
		}

		bits := routingTable.bitsAtDepth(node.depth)
		for childIndex, child := range node.children {
			childPrefix := prefix
			// The child index is the next b-bit prefix segment, so setting those
			// bits gives a stable representative ID for that subtree.
			setBits(&childPrefix, node.depth, bits, childIndex)
			walk(child, childPrefix)
		}
	}

	var prefix KademliaID
	walk(routingTable.root, prefix)
	return values
}

// The routing tree is binary for every b. Parameter b determines which
// non-local full buckets may split: depth % b != 0 (supplementary lab PDF).
func (routingTable *RoutingTable) bitsAtDepth(depth int) int {
	if depth >= IDLength*8 {
		return 0
	}
	return 1
}

func (routingTable *RoutingTable) childIndex(id *KademliaID, depth int) int {
	return readBits(id, depth, routingTable.bitsAtDepth(depth))
}

func readBits(id *KademliaID, offset int, count int) int {
	value := 0
	for i := 0; i < count; i++ {
		// Read from most significant to least significant bit so child indexes
		// match the visual binary prefix order used in Kademlia explanations.
		value = (value << 1) | bitAt(id, offset+i)
	}
	return value
}

func setBits(id *KademliaID, offset int, count int, value int) {
	for i := 0; i < count; i++ {
		shift := count - 1 - i
		setBit(id, offset+i, (value>>shift)&1)
	}
}

func bitAt(id *KademliaID, bitIndex int) int {
	byteIndex := bitIndex / 8
	shift := uint8(7 - bitIndex%8)
	return int((id[byteIndex] >> shift) & 1)
}

func setBit(id *KademliaID, bitIndex int, bit int) {
	byteIndex := bitIndex / 8
	shift := uint8(7 - bitIndex%8)
	if bit == 1 {
		id[byteIndex] |= 1 << shift
		return
	}
	id[byteIndex] &^= 1 << shift
}

func (rt *RoutingTable) child(node *routingTreeNode, id *KademliaID) *routingTreeNode {
	return node.children[rt.childIndex(id, node.depth)]
}

// SetCapacity configures k before use, retaining the most recently seen contacts.
func (rt *RoutingTable) SetCapacity(k int) {
	if k <= 0 {
		k = bucketSize
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.capacity == k {
		return
	}
	rt.capacity = k
	collectLeafValues(rt, func(_ int, n *routingTreeNode, _ KademliaID) (int, bool) {
		n.bucket.capacity = k
		for n.bucket.Len() > k {
			n.bucket.list.Remove(n.bucket.list.Back())
		}
		return 0, false
	})
}

// Offer returns the least recently seen contact when an unsplittable bucket is full.
// Network liveness checks happen outside the routing-table lock.
func (rt *RoutingTable) Offer(c Contact) (Contact, time.Time, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if c.ID == nil || c.ID.Equals(rt.me.ID) {
		return Contact{}, time.Time{}, false
	}
	rt.add(c)
	n := rt.root
	for !n.isLeaf() {
		n = rt.child(n, c.ID)
	}
	if n.bucket.hasContact(c) {
		return Contact{}, time.Time{}, false
	}
	return n.bucket.list.Back().Value.(Contact), n.updated, true
}

// ReplaceDead avoids evicting a bucket that has received newer traffic since probing.
func (rt *RoutingTable) ReplaceDead(old, replacement Contact, observed time.Time) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	n := rt.root
	for !n.isLeaf() {
		n = rt.child(n, old.ID)
	}
	if !n.updated.Equal(observed) {
		return
	}
	for e := n.bucket.list.Front(); e != nil; e = e.Next() {
		if e.Value.(Contact).ID.Equals(old.ID) {
			n.bucket.list.Remove(e)
			break
		}
	}
	rt.add(replacement)
}

func (rt *RoutingTable) Remove(c Contact) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if c.ID == nil {
		return
	}
	n := rt.root
	for !n.isLeaf() {
		n = rt.child(n, c.ID)
	}
	for e := n.bucket.list.Front(); e != nil; e = e.Next() {
		if e.Value.(Contact).ID.Equals(c.ID) {
			n.bucket.list.Remove(e)
			return
		}
	}
}

// RefreshTargets snapshots random IDs from stale materialized leaf ranges.
// Every leaf range, including empty ranges, is represented explicitly.
func (rt *RoutingTable) RefreshTargets(age time.Duration) []*KademliaID {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return collectLeafValues(rt, func(_ int, n *routingTreeNode, prefix KademliaID) (*KademliaID, bool) {
		if age > 0 && time.Since(n.updated) < age {
			return nil, false
		}
		target := prefix
		var random KademliaID
		if _, err := rand.Read(random[:]); err != nil {
			return nil, false
		}
		for bit := n.depth; bit < IDLength*8; bit++ {
			setBit(&target, bit, bitAt(&random, bit))
		}
		n.updated = time.Now()
		return &target, true
	})
}

func (rt *RoutingTable) Snapshot() map[int][]Contact {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	result := make(map[int][]Contact)
	collectLeafValues(rt, func(i int, n *routingTreeNode, _ KademliaID) (int, bool) {
		if n.bucket.Len() > 0 {
			result[i] = n.bucket.Contacts()
		}
		return 0, false
	})
	return result
}
