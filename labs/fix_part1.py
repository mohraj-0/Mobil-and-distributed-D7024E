from pathlib import Path
p=Path('kademlia/kademlia.go')
s=p.read_text(encoding='utf-8')
s=s.replace('"fmt"','"fmt"\n "log"')
s=s.replace('RPCTimeout time.Duration','RPCTimeout time.Duration\n ReplicationPeriod time.Duration\n RefreshPeriod time.Duration\n maintenanceMu sync.Mutex\n maintenanceStop chan struct{}\n maintenanceDone chan struct{}\n evictionMu sync.Mutex\n evictions map[string]bool\n pingMu sync.Mutex\n pingResponses map[string]chan struct{}')
s=s.replace('Contacts []Contact\n}', 'Contacts []Contact\n Source Contact\n}')
start=s.index('func (kademlia *Kademlia) addContact(')
end=s.index('\nfunc (kademlia *Kademlia) closestContacts',start)
s=s[:start]+'''func (kademlia *Kademlia) addContact(contact Contact) {
 if kademlia.RoutingTable == nil || contact.ID == nil { return }
 kademlia.RoutingTable.SetCapacity(kademlia.kValue())
 old, observed, full := kademlia.RoutingTable.Offer(contact)
 if !full || kademlia.Network == nil { return }
 kademlia.evictionMu.Lock()
 if kademlia.evictions == nil { kademlia.evictions = make(map[string]bool) }
 id := old.ID.String()
 if kademlia.evictions[id] { kademlia.evictionMu.Unlock(); return }
 kademlia.evictions[id] = true
 kademlia.evictionMu.Unlock()
 go func(){
  defer func(){ kademlia.evictionMu.Lock(); delete(kademlia.evictions,id); kademlia.evictionMu.Unlock() }()
  if _,err := kademlia.Ping(old); err != nil { kademlia.RoutingTable.ReplaceDead(old,contact,observed) }
 }()
}
''' +s[end:]
s=s.replace('case rpcFindNode:\n', 'case "PING":\n kademlia.handlePing(rpc)\n case "PING_REPLY":\n kademlia.handlePingReply(rpc)\n case rpcFindNode:\n',1)
# Ignore unsolicited replies before they can alter the routing table.
for method,mu,responses in [('handleFindNodeReplyRPC','findNodeMu','findNodeResponses'),('handleFindValueReplyRPC','findValueMu','findValueResponses'),('handleStoreReplyRPC','storeMu','storeResponses')]:
 marker=f'func (kademlia *Kademlia) {method}(rpc rpcMessage) {{'
 s=s.replace(marker,marker+f'\n kademlia.{mu}.Lock()\n pending := kademlia.{responses}[rpc.RequestID] != nil\n kademlia.{mu}.Unlock()\n if !pending {{ return }}')
s=s.replace('Contacts: wireToContacts(rpc.Contacts),','Contacts: wireToContacts(rpc.Contacts),\n Source: NewContact(NewKademliaID(rpc.SenderID),rpc.SenderAddress),')
# Preserve API while adding a concurrency-safe per-call source result.
start=s.index('func (kademlia *Kademlia) LookupData(')
end=s.index('// Lagrar data',start)
body=s[start:end]
body=body.replace('func (kademlia *Kademlia) LookupData(hash string) ([]byte, error) {','''func (kademlia *Kademlia) LookupData(hash string) ([]byte, error) {
 value,_,err := kademlia.LookupDataFrom(hash)
 return value,err
}

func (kademlia *Kademlia) LookupDataFrom(hash string) (value []byte, source Contact, err error) {
 defer func(){ log.Printf("event=lookup_result type=value key=%s success=%t error=%q",hash,err==nil,errText(err)) }()
''')
body=body.replace('return nil, fmt.Errorf(', 'return nil, Contact{}, fmt.Errorf(')
body=body.replace('return localValue, nil','return localValue, kademlia.me(), nil')
body=body.replace('return append([]byte(nil), response.result.Value...), nil','return append([]byte(nil), response.result.Value...), response.result.Source, nil')
body=body.replace('err    error\n', 'err    error\n contact Contact\n')
body=body.replace('lookupResult{result: result, err: err}', 'lookupResult{result: result, err: err, contact: contact}')
body=body.replace('if response.err != nil {\n\t\t\t\tcontinue','if response.err != nil {\n kademlia.RoutingTable.Remove(response.contact)\n failed[response.contact.ID.String()]=true\n\t\t\t\tcontinue')
body=body.replace('queried := make(map[string]bool)','queried := make(map[string]bool)\n failed := make(map[string]bool)')
body=body.replace('if queried[id] {','if queried[id] || failed[id] {')
body=body.replace('if seen[id] {','if seen[id] || failed[id] {')
body=body.replace('\n\t\t\t\tcontinue\n\t\t\t}\n\n\t\t\t// Om värdet', '\n log.Printf("event=hash_error key=%s source=%s",hash,response.contact.Address)\n\t\t\t\tcontinue\n\t\t\t}\n\n\t\t\t// Om värdet')
body=body.replace('candidates = firstContacts(candidates, kademlia.kValue())','candidates = liveCandidates(candidates,failed)')
body=body.replace('\n\t}\n\n\tcandidates :=', '\n log.Printf("event=hash_error key=%s source=local",hash)\n\t}\n\n\tcandidates :=',1)
s=s[:start]+body+s[end:]
# Probe logs include retransmissions, as actual wire traffic.
for method in ['sendFindNodeRPC','sendFindValueRPC']:
 start=s.index('func (kademlia *Kademlia) '+method)
 end=s.index('\n//', start)
 chunk=s[start:end].replace('err = kademlia.Network.SendData(contact.Address, data)','log.Printf("event=probe rpc=%s target=%s attempt=%d request_id=%s",request.Type,contact.Address,attempt+1,requestID)\n err = kademlia.Network.SendData(contact.Address, data)')
 s=s[:start]+chunk+s[end:]
# Direct bootstrap liveness check makes failed joins fail.
s=s.replace('kademlia.addContact(bootstrap)\n\tme :=', 'if _,err := kademlia.Ping(bootstrap); err != nil { return fmt.Errorf("bootstrap unreachable: %w",err) }\n kademlia.addContact(bootstrap)\n\tme :=')
s=s.replace('\n\treturn nil\n}\n\n// Refresh gör', '\n for _,id := range kademlia.RoutingTable.RefreshTargets(0) { kademlia.LookupContact(&Contact{ID:id}) }\n\treturn nil\n}\n\n// Refresh gör')
# Replace lookup algorithm; retain fallback candidates beyond k and exclude failed probes.
start=s.index('func (kademlia *Kademlia) lookupContactParallel(')
end=s.index('// Frågar flera',start)
s=s[:start]+'''func (kademlia *Kademlia) lookupContactParallel(target *KademliaID, count int) (answer []Contact) {
 if count<=0 || target==nil || kademlia.RoutingTable==nil || kademlia.Network==nil { return nil }
 defer func(){ log.Printf("event=lookup_result type=node target=%s success=%t",target,len(answer)>0) }()
 kademlia.RoutingTable.SetCapacity(kademlia.kValue())
 candidates := kademlia.closestContacts(target,count*kademlia.alpha())
 seen,queried,failed := map[string]bool{},map[string]bool{},map[string]bool{}
 me := kademlia.me()
 for _,c := range candidates { seen[c.ID.String()]=true; c.CalcDistance(target) }
 for {
  candidates=liveCandidates(candidates,failed)
  sortContactsByDistance(candidates)
  shortlist:=firstContacts(candidates,count)
  batch:=[]Contact{}
  for _,c := range shortlist { if !queried[c.ID.String()] { batch=append(batch,c); if len(batch)==kademlia.alpha(){break} } }
  if len(batch)==0 { return shortlist }
  type result struct { contact Contact; contacts []Contact; err error }
  results:=make(chan result,len(batch))
  for _,c:=range batch { queried[c.ID.String()]=true; go func(c Contact){ contacts,err:=kademlia.sendFindNodeRPC(c,target); results<-result{c,contacts,err} }(c) }
  for range batch {
   r:=<-results
   if r.err!=nil { failed[r.contact.ID.String()]=true; kademlia.RoutingTable.Remove(r.contact); continue }
   for _,c:=range r.contacts {
    if c.ID==nil || c.ID.Equals(me.ID) || seen[c.ID.String()] { continue }
    seen[c.ID.String()]=true; c.CalcDistance(target); candidates=append(candidates,c)
   }
  }
 }
}

func liveCandidates(contacts []Contact, failed map[string]bool) []Contact {
 result:=make([]Contact,0,len(contacts))
 for _,c:=range contacts { if c.ID!=nil && !failed[c.ID.String()] { result=append(result,c) } }
 return result
}

''' +s[end:]
p.write_text(s,encoding='utf-8')
