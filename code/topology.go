package main

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// ---- /topology
//
// SPR merges each plugin's graph into the router topology view at the "root"
// anchor node. The struct shapes mirror spr-tailscale exactly.

type TopoNode struct {
	ID       string
	Kind     string
	Name     string
	IP       string `json:",omitempty"`
	ConnType string `json:",omitempty"`
	Online   bool
}

type TopoEdge struct {
	From  string
	To    string
	Layer string
	Kind  string
}

type Topology struct {
	Nodes []TopoNode
	Edges []TopoEdge
}

// topologyRoot is the anchor the SPR host grafts the plugin graph onto.
func topologyRoot() TopoNode {
	return TopoNode{ID: "root", ConnType: "tailscale", Online: true}
}

// tailnetIP picks the address shown on the topology node: the IPv4 tailnet
// address when present, otherwise the first address headscale assigned.
func tailnetIP(ips []string) string {
	for _, ip := range ips {
		if strings.Contains(ip, ".") {
			return ip
		}
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return ""
}

// buildTopology maps headscale nodes onto the SPR topology graph: one device
// node per headscale node, each edged toward the root anchor. Pure function
// over the plugin's NodeInfo shape so it is unit-testable from CLI fixtures.
func buildTopology(nodes []NodeInfo) Topology {
	topo := Topology{
		Nodes: []TopoNode{topologyRoot()},
		Edges: []TopoEdge{},
	}
	for _, n := range nodes {
		id := "headscale-" + strconv.FormatUint(n.ID, 10)
		name := n.GivenName
		if name == "" {
			name = n.Name
		}
		topo.Nodes = append(topo.Nodes, TopoNode{
			ID:       id,
			Kind:     "device",
			Name:     name,
			IP:       tailnetIP(n.IPs),
			ConnType: "tailscale",
			Online:   n.Online,
		})
		topo.Edges = append(topo.Edges, TopoEdge{From: id, To: "root", Layer: "tailscale", Kind: "tailscale"})
	}
	devices := topo.Nodes[1:] // keep the root anchor first
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Name != devices[j].Name {
			return devices[i].Name < devices[j].Name
		}
		return devices[i].ID < devices[j].ID
	})
	sort.Slice(topo.Edges, func(i, j int) bool { return topo.Edges[i].From < topo.Edges[j].From })
	return topo
}

// handleGetTopology returns the live graph; when headscale is down (or the
// CLI fails) it degrades to the bare root anchor instead of erroring, so the
// router topology view stays intact.
func handleGetTopology(w http.ResponseWriter, r *http.Request) {
	topo := Topology{Nodes: []TopoNode{topologyRoot()}, Edges: []TopoEdge{}}
	if gDaemon.Running() {
		if nodes, err := cliListNodes(r.Context()); err == nil {
			topo = buildTopology(nodes)
		}
	}
	jsonOK(w, topo)
}
