from pathlib import Path
p=Path('experiments/main.go');s=p.read_text(encoding='utf-8')
a=s.index('\tfor i := range nodes {\n\t\tfor j := range nodes {',s.index('func buildRandomNetwork'))
b=s.index('\n\treturn nodes, nil',a)
s=s[:a]+''' for _,entry:=range nodes { entry.node.RoutingTable.SetCapacity(kValue); go entry.node.ListenForRPC() }
 for i:=1;i<len(nodes);i++ {
  if err:=nodes[i].node.Join(nodes[0].contact);err!=nil { closeNodes(nodes);return nil,fmt.Errorf("join node %d: %w",i,err) }
 }
''' +s[b:]
s=s.replace('sum := sha256.Sum256([]byte(text))','host,port,err:=net.SplitHostPort(text)\n if err!=nil { return nil }\n sum := sha256.Sum256([]byte(host+"|"+port))')
s=s.replace('"math"','"math"\n "net"')
s=s.replace('routing tables are pre-populated with all contacts during the experiment setup, so node lookups often approach `k` probes: the lookup already knows many close contacts and then queries the closest shortlist.', 'nodes join through a bootstrap using the actual joining procedure; routing tables are learned through RPCs rather than pre-populated with global membership. Expected sequential lookup rounds grow approximately as log2(N), while probe counts also reflect alpha, k and final shortlist verification.')
s=s.replace('often because the fully populated routing tables make the shortlist deterministic in size.', 'possibly because k and final shortlist verification dominate at the tested sizes.')
s=s.replace('pre-populated routing tables', 'joined routing tables')
s=s.replace('lossless simulated network with pre-populated routing tables', 'lossless simulated network with joined routing tables')
s=s.replace('repeatable', 'repeatable')
p.write_text(s,encoding='utf-8')
p=Path('experiments/resilience.go');s=p.read_text(encoding='utf-8')
# Configure bucket capacity before adding contacts.
s=s.replace('return experimentNode{node: node, contact: contact, network: transport}, nil','node.RoutingTable.SetCapacity(s.k)\n return experimentNode{node: node, contact: contact, network: transport}, nil')
a=s.index('\tfor i := range nodes {\n\t\tfor j := range nodes {',s.index('func resilienceTrial'))
b=s.index('\n\tvalue :=',a)
s=s[:a]+''' for _,entry:=range nodes { go entry.node.ListenForRPC() }
 for i:=1;i<len(nodes);i++ { if err:=nodes[i].node.Join(nodes[0].contact);err!=nil{return trialResult{},err} }
''' +s[b:]
# Replacements join the live network; survivors discover them through normal traffic.
a=s.index('\t// Seed replacement routing tables')
b=s.index('\n\tc.mu.Lock()',a)
s=s[:a]+''' // Start replacement listeners, then join through another current member.
 if result.joins>0 {
  for i:=range nodes {if strings.HasPrefix(nodes[i].contact.Address,"172.16."){go nodes[i].node.ListenForRPC()}}
  for i:=range nodes {if strings.HasPrefix(nodes[i].contact.Address,"172.16."){if err:=nodes[i].node.Join(nodes[(i+1)%len(nodes)].contact);err!=nil{return result,err}}}
 }
''' +s[b:]
# Report variance in addition to mean for mandatory experiment.
s=s.replace('| mean probes | mean elapsed ms |','| mean probes | probe variance | success variance | mean elapsed ms |')
s=s.replace('| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |','| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |')
s=s.replace('| %.4f | %.4f | %.4f | %s | %s |\\n", s.axis', '| %.4f | %.4f | %.4f | %.4f | %.4f | %s | %s |\\n", s.axis')
s=s.replace('summary.ProbeMean, elapsed/float64(summary.Count)', 'summary.ProbeMean, summary.probeVariance(), successVariance(summary.Successes,summary.Count), elapsed/float64(summary.Count)')
s += '''
func successVariance(successes,count int) float64 {
 if count<2{return 0};p:=float64(successes)/float64(count);return p*(1-p)*float64(count)/float64(count-1)
}
'''
p.write_text(s,encoding='utf-8')
