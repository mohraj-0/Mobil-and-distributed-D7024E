from pathlib import Path
p=Path('cmd/simshell/main.go'); s=p.read_text(encoding='utf-8')
s=s.replace('"strings"','"strings"\n "sort"')
s=s.replace('transport     string','alpha,k int\n replication,refresh time.Duration\n transport     string')
marker='root.PersistentFlags().StringVar(&s.opts.transport'
i=s.index(marker)
s=s[:i]+'''root.PersistentFlags().IntVar(&s.opts.alpha,"alpha",3,"parallel lookup probes")
 root.PersistentFlags().IntVar(&s.opts.k,"k",10,"bucket capacity and replica count")
 root.PersistentFlags().DurationVar(&s.opts.replication,"replication-period",time.Hour,"replica repair interval")
 root.PersistentFlags().DurationVar(&s.opts.refresh,"refresh-period",time.Hour,"stale bucket refresh interval")
 '''+s[i:]
# Configure before listeners start, and start maintenance after joining.
s=s.replace('go node.ListenForRPC()', '''if s.opts.alpha>0 { node.Alpha=s.opts.alpha }
 if s.opts.k>0 { node.K=s.opts.k }
 node.ReplicationPeriod,node.RefreshPeriod=s.opts.replication,s.opts.refresh
 node.RoutingTable.SetCapacity(node.K)
 go node.ListenForRPC()''')
s=s.replace('nodes = append(nodes, node)','node.StartMaintenance()\n nodes = append(nodes, node)')
s=s.replace('if s.opts.bootstrapAddr == "" {\n\t\treturn nil','if s.opts.bootstrapAddr == "" {\n node.StartMaintenance()\n\t\treturn nil')
s=s.replace('return node.Join(kademlia.NewContact(id, s.opts.bootstrapAddr))','if err:=node.Join(kademlia.NewContact(id,s.opts.bootstrapAddr)); err!=nil { return err }; node.StartMaintenance(); return nil')
a=s.index('\tstart := time.Now()',s.index('func (s *shell) ping(')); b=s.index('\n}\n',a)
s=s[:a]+''' rtt,err:=s.node.Ping(kademlia.Contact{Address:addr})
 if err!=nil { return err }
 fmt.Printf("pong from %s in %s\\n",addr,rtt)
 return nil'''+s[b:]
s=s.replace('data, err := s.node.LookupData(key)','data, source, err := s.node.LookupDataFrom(key)')
s=s.replace('if filename != "" {\n\t\tif err := os.WriteFile','fmt.Printf("received from %s id=%s\\n",source.Address,shortID(source.ID))\n if filename != "" {\n\t\tif err := os.WriteFile')
s=s.replace('for _, index := range s.node.RoutingTable.NonEmptyBucketIndices() {\n\t\t\tcontacts := s.node.RoutingTable.ContactsInBucket(index)', '''buckets:=s.node.RoutingTable.Snapshot()
  indices:=make([]int,0,len(buckets)); for index:=range buckets { indices=append(indices,index) }; sort.Ints(indices)
  for _, index := range indices {
   contacts := buckets[index]''')
s=s.replace('if len(s.node.RoutingTable.NonEmptyBucketIndices()) == 0 {','if len(buckets) == 0 {')
s=s.replace('case "ds":\n\t\tif len(s.node.DataStore)', 'case "ds":\n dataStore:=s.node.DataSnapshot()\n\t\tif len(dataStore)')
s=s.replace('for key, data := range s.node.DataStore {','for key, data := range dataStore {')
s=s.replace('if s.network != nil {\n\t\t_ = s.network.Close()', 'if s.network != nil {\n\t\t_ = s.network.Close()\n if s.node!=nil { s.node.StopMaintenance() }')
s=s.replace('_ = node.Network.Close()','_ = node.Network.Close()\n node.StopMaintenance()')
p.write_text(s,encoding='utf-8')
# Existing bootstrap mocks need to answer the new real liveness check.
p=Path('kademlia/kademlia_test.go');s=p.read_text(encoding='utf-8')
start=s.index('func TestJoinSuccess');end=s.index('func TestHandleRPCMessage',start)
chunk=s[start:end].replace('if request.Type == rpcFindNode {','if request.Type == "PING" { node.handlePingReply(rpcMessage{RequestID:request.RequestID,SenderID:bootstrap.ID.String(),SenderAddress:bootstrap.Address}) }\n if request.Type == rpcFindNode {')
chunk=chunk.replace('want := IDLength*8 + 1','want := IDLength*8 + 2').replace('self lookup plus 256 bucket refreshes','self lookup plus XOR and leaf bucket refreshes')
s=s[:start]+chunk+s[end:];p.write_text(s,encoding='utf-8')
