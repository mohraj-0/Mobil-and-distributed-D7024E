package kademlia

const bucketSize = 20

// RoutingTable lagrar kända kontakter i fasta buckets baserade på XOR-avstånd.
// me är den lokala noden; alla bucket-index räknas relativt till me.ID.
type RoutingTable struct {
	me      Contact
	buckets [IDLength * 8]*bucket
}

// NewRoutingTable skapar 256 buckets, en för varje möjlig första skiljande bit
// i ett 256-bitars Kademlia-ID.
func NewRoutingTable(me Contact) *RoutingTable {
	routingTable := &RoutingTable{}
	for i := 0; i < IDLength*8; i++ {
		routingTable.buckets[i] = newBucket()
	}
	routingTable.me = me
	return routingTable
}

// AddContact lägger en kontakt i den bucket som motsvarar kontaktens XOR-avstånd
// från den lokala nodens ID.
func (routingTable *RoutingTable) AddContact(contact Contact) {
	bucketIndex := routingTable.getBucketIndex(contact.ID)
	bucket := routingTable.buckets[bucketIndex]
	bucket.AddContact(contact)
}

// FindClosestContacts hämtar de count närmaste kontakterna till target.
// Först undersöks targetens egen bucket, sedan närliggande buckets åt båda håll,
// och till sist sorteras alla kandidater efter exakt XOR-avstånd.
func (routingTable *RoutingTable) FindClosestContacts(target *KademliaID, count int) []Contact {
	var candidates ContactCandidates
	bucketIndex := routingTable.getBucketIndex(target)
	bucket := routingTable.buckets[bucketIndex]

	// Börja där target-ID:t skulle hamna relativt till vår egen nod.
	candidates.Append(bucket.GetContactAndCalcDistance(target))

	// Om den bucketen inte räcker, samla kontakter från buckets bredvid.
	for i := 1; (bucketIndex-i >= 0 || bucketIndex+i < IDLength*8) && candidates.Len() < count; i++ {
		if bucketIndex-i >= 0 {
			bucket = routingTable.buckets[bucketIndex-i]
			candidates.Append(bucket.GetContactAndCalcDistance(target))
		}
		if bucketIndex+i < IDLength*8 {
			bucket = routingTable.buckets[bucketIndex+i]
			candidates.Append(bucket.GetContactAndCalcDistance(target))
		}
	}

	candidates.Sort()

	if count > candidates.Len() {
		count = candidates.Len()
	}

	return candidates.GetContacts(count)
}

// getBucketIndex hittar vilken bucket ett ID tillhör.
// Indexet är positionen för första 1-bit i XOR-avståndet mellan id och me.ID.
func (routingTable *RoutingTable) getBucketIndex(id *KademliaID) int {
	distance := id.CalcDistance(routingTable.me.ID)
	for i := 0; i < IDLength; i++ {
		for j := 0; j < 8; j++ {
			if (distance[i]>>uint8(7-j))&0x1 != 0 {
				return i*8 + j
			}
		}
	}

	return IDLength*8 - 1
}
