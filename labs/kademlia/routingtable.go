package kademlia

const bucketSize = 20
const defaultRoutingTreeBranchBits = 1

// RoutingTable stores known contacts in a Kademlia routing tree. Leaf nodes are
// k-buckets, and an overflowing leaf is split only when its range contains the
// local node ID.
type RoutingTable struct {
	me         Contact
	root       *routingTreeNode
	branchBits int
}

type routingTreeNode struct {
	// A node is a leaf when bucket is non-nil. Internal nodes set bucket to nil
	// and route contacts into children according to the next branchBits bits.
	bucket   *bucket
	children []*routingTreeNode

	// depth is the number of ID prefix bits already consumed on the path from
	// the root to this node. For b=2, depths are 0, 2, 4, ...
	depth int

	// Kademlia only keeps splitting the bucket that contains the local node.
	// Other full buckets keep at most k contacts.
	containsMe bool
}

// NewRoutingTable creates a routing tree with b = 1, which is the standard
// binary Kademlia tree and the simplified option allowed by the lab spec.
func NewRoutingTable(me Contact) *RoutingTable {
	return NewRoutingTableWithBranchBits(me, defaultRoutingTreeBranchBits)
}

// NewRoutingTableWithBranchBits creates a generalized routing tree where b is
// the number of bits consumed at each tree level. The branching factor is 2^b.
func NewRoutingTableWithBranchBits(me Contact, branchBits int) *RoutingTable {
	if branchBits < 1 {
		branchBits = 1
	}
	if branchBits > IDLength*8 {
		branchBits = IDLength * 8
	}

	return &RoutingTable{
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
	}
}

// AddContact inserts a contact into the appropriate leaf bucket. If that bucket
// is full and contains this node, the bucket is split and insertion is retried.
func (routingTable *RoutingTable) AddContact(contact Contact) {
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
		if node.bucket.Len() < bucketSize || node.bucket.hasContact(contact) || !node.canSplit() {
			node.bucket.AddContact(contact)
			return
		}

		// The only remaining case is a full leaf bucket on the local node's
		// prefix path, so split it and retry insertion in the new child.
		routingTable.split(node)
		routingTable.addContact(node, contact)
		return
	}

	childIndex := routingTable.childIndex(contact.ID, node.depth)
	routingTable.addContact(node.children[childIndex], contact)
}

func (node *routingTreeNode) isLeaf() bool {
	return node.bucket != nil
}

func (node *routingTreeNode) canSplit() bool {
	return node.containsMe && node.depth < IDLength*8
}

func (routingTable *RoutingTable) split(node *routingTreeNode) {
	bits := routingTable.bitsAtDepth(node.depth)
	childCount := 1 << bits
	children := make([]*routingTreeNode, childCount)
	meChildIndex := routingTable.childIndex(routingTable.me.ID, node.depth)

	for i := range children {
		children[i] = newRoutingTreeNode(node.depth+bits, i == meChildIndex)
	}

	contacts := node.bucket.Contacts()
	node.bucket = nil
	node.children = children

	// Redistribute the old bucket contents using the same prefix bits that will
	// route future contacts into these children.
	for _, contact := range contacts {
		childIndex := routingTable.childIndex(contact.ID, node.depth)
		children[childIndex].bucket.AddContact(contact)
	}
}

// FindClosestContacts returns the count contacts with smallest XOR distance to
// target. The tree is used for storage; the final result is sorted by distance.
func (routingTable *RoutingTable) FindClosestContacts(target *KademliaID, count int) []Contact {
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
	return collectLeafValues(routingTable, func(index int, node *routingTreeNode, _ KademliaID) (int, bool) {
		if node.bucket.Len() == 0 {
			return 0, false
		}
		return index, true
	})
}

// ContactsInBucket returns the current contents of one leaf bucket.
func (routingTable *RoutingTable) ContactsInBucket(bucketIndex int) []Contact {
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

// RefreshIDForBucket returns a representative ID inside a leaf bucket's range.
func (routingTable *RoutingTable) RefreshIDForBucket(bucketIndex int) *KademliaID {
	matches := collectLeafValues(routingTable, func(index int, _ *routingTreeNode, prefix KademliaID) (*KademliaID, bool) {
		if index != bucketIndex {
			return nil, false
		}
		target := prefix
		return &target, true
	})
	if len(matches) == 0 {
		return routingTable.me.ID
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

func (routingTable *RoutingTable) bitsAtDepth(depth int) int {
	remaining := IDLength*8 - depth
	// The last level may have fewer than b bits if b does not divide B.
	if remaining < routingTable.branchBits {
		return remaining
	}
	return routingTable.branchBits
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
