from pathlib import Path
p=Path('kademlia/routingtable.go');s=p.read_text(encoding='utf-8')
s=s.replace('\n\t"sort"','')
s=s.replace('\n\tsparse   map[KademliaID]*routingTreeNode','')
s=s.replace('\n\tsparse map[KademliaID]*routingTreeNode','')
s=s.replace('!node.canSplit()', '!routingTable.canSplit(node)')
a=s.index('func (node *routingTreeNode) canSplit()');b=s.index('\nfunc (routingTable *RoutingTable) split',a)
s=s[:a]+'''func (rt *RoutingTable) canSplit(node *routingTreeNode) bool {
 return node.depth < IDLength*8 && (node.containsMe || node.depth%rt.branchBits != 0)
}
''' +s[b:]
a=s.index('\n\tif bits > 8 {');b=s.index('\n\tchildCount :=',a);s=s[:a]+s[b:]
s=s.replace('i == meChildIndex)', 'node.containsMe && i == meChildIndex)')
# Only accepted contacts count as bucket activity.
s=s.replace('node.updated = time.Now()', 'if node.bucket.hasContact(contact) { node.updated = time.Now() }',1)
a=s.index('\n\t\tif node.sparse != nil {');b=s.index('\n\t\tfor childIndex, child :=',a);s=s[:a]+s[b:]
a=s.index('func (routingTable *RoutingTable) bitsAtDepth(');b=s.index('\nfunc (routingTable *RoutingTable) childIndex',a)
s=s[:a]+'''// The routing tree is binary for every b. Parameter b determines which
// non-local full buckets may split: depth % b != 0 (supplementary lab PDF).
func (routingTable *RoutingTable) bitsAtDepth(depth int) int {
 if depth >= IDLength*8 { return 0 }; return 1
}
''' +s[b:]
a=s.index('func (rt *RoutingTable) child(');b=s.index('// SetCapacity',a)
s=s[:a]+'''func (rt *RoutingTable) child(node *routingTreeNode, id *KademliaID) *routingTreeNode {
 return node.children[rt.childIndex(id,node.depth)]
}

''' +s[b:]
s=s.replace('The branching factor is 2^b.', 'Splits remain binary; b permits extra splits at depths not divisible by b.')
s=s.replace('the number of bits consumed at each tree level', 'the acceleration parameter from the lab routing-tree supplement')
s=s.replace('Sparse trees represent unmaterialized ranges implicitly; Refresh also probes\n// all 256 XOR-distance ranges when joining.', 'Every leaf range, including empty ranges, is represented explicitly.')
s=s.replace('Leaf nodes are\n// k-buckets, and an overflowing leaf is split only when its range contains the\n// local node ID.', 'Leaf nodes are\n// k-buckets; full leaves split when they contain the local ID or depth%b != 0.')
s=s.replace('If that bucket\n// is full and contains this node, the bucket is split and insertion is retried.', 'If that bucket\n// is full and may split according to b, it splits and insertion is retried.')
p.write_text(s,encoding='utf-8')
