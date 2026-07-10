package main

import (
	"reflect"
	"testing"
)

// nodesFromCLI runs a raw `headscale nodes list -o json` document through the
// same decode+mapping pipeline the live handler uses.
func nodesFromCLI(t *testing.T, data string) []NodeInfo {
	t.Helper()
	raw, err := decodeList[hsNode]([]byte(data))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	out := make([]NodeInfo, 0, len(raw))
	for _, n := range raw {
		out = append(out, nodeInfo(n))
	}
	return out
}

// cliNodesFixture is shaped like the headscale CLI's protobuf-JSON output:
// snake_case fields, pbTime timestamps. Deliberately unsorted by name.
const cliNodesFixture = `[
  {
    "id": 2,
    "node_key": "nodekey:bbb",
    "ip_addresses": ["fd7a:115c:a1e0::2", "100.64.0.2"],
    "name": "zulu-laptop",
    "user": {"id": 1, "name": "alex"},
    "last_seen": {"seconds": 1751900000, "nanos": 0},
    "given_name": "",
    "online": true
  },
  {
    "id": 7,
    "node_key": "nodekey:ccc",
    "ip_addresses": ["100.64.0.7"],
    "name": "alex-phone.internal",
    "user": {"id": 1, "name": "alex"},
    "last_seen": {"seconds": 1751000000, "nanos": 0},
    "given_name": "alex-phone",
    "online": false
  },
  {
    "id": 3,
    "node_key": "nodekey:ddd",
    "ip_addresses": [],
    "name": "no-ip-yet",
    "user": {"id": 2, "name": "guest"},
    "given_name": "no-ip-yet",
    "online": false
  }
]`

func TestBuildTopologyFromCLIFixture(t *testing.T) {
	topo := buildTopology(nodesFromCLI(t, cliNodesFixture))

	wantNodes := []TopoNode{
		{ID: "root", ConnType: "tailscale", Online: true},
		// devices sorted by name after the root anchor
		{ID: "headscale-7", Kind: "device", Name: "alex-phone", IP: "100.64.0.7", ConnType: "tailscale", Online: false},
		{ID: "headscale-3", Kind: "device", Name: "no-ip-yet", IP: "", ConnType: "tailscale", Online: false},
		// given_name empty -> falls back to hostname; IPv4 preferred over IPv6
		{ID: "headscale-2", Kind: "device", Name: "zulu-laptop", IP: "100.64.0.2", ConnType: "tailscale", Online: true},
	}
	if !reflect.DeepEqual(topo.Nodes, wantNodes) {
		t.Errorf("nodes mismatch:\n got %+v\nwant %+v", topo.Nodes, wantNodes)
	}

	wantEdges := []TopoEdge{
		{From: "headscale-2", To: "root", Layer: "tailscale", Kind: "tailscale"},
		{From: "headscale-3", To: "root", Layer: "tailscale", Kind: "tailscale"},
		{From: "headscale-7", To: "root", Layer: "tailscale", Kind: "tailscale"},
	}
	if !reflect.DeepEqual(topo.Edges, wantEdges) {
		t.Errorf("edges mismatch:\n got %+v\nwant %+v", topo.Edges, wantEdges)
	}
}

func TestBuildTopologyEmpty(t *testing.T) {
	// the CLI prints `null` for empty lists; daemon-down uses the same shape
	for _, fixture := range []string{"null", "", "[]"} {
		topo := buildTopology(nodesFromCLI(t, fixture))
		if len(topo.Nodes) != 1 || topo.Nodes[0].ID != "root" {
			t.Errorf("fixture %q: want root-only nodes, got %+v", fixture, topo.Nodes)
		}
		root := topo.Nodes[0]
		if root.ConnType != "tailscale" || !root.Online {
			t.Errorf("fixture %q: bad root anchor %+v", fixture, root)
		}
		if len(topo.Edges) != 0 {
			t.Errorf("fixture %q: want no edges, got %+v", fixture, topo.Edges)
		}
	}
}

func TestBuildTopologyEdgesReferenceExistingNodes(t *testing.T) {
	topo := buildTopology(nodesFromCLI(t, cliNodesFixture))
	ids := map[string]bool{}
	for _, n := range topo.Nodes {
		if ids[n.ID] {
			t.Errorf("duplicate node id %q", n.ID)
		}
		ids[n.ID] = true
	}
	for _, e := range topo.Edges {
		if !ids[e.From] || !ids[e.To] {
			t.Errorf("edge %+v references unknown node", e)
		}
		if e.To != "root" {
			t.Errorf("edge %+v must point toward root", e)
		}
	}
	if want := len(topo.Nodes) - 1; len(topo.Edges) != want {
		t.Errorf("want %d edges, got %d", want, len(topo.Edges))
	}
}

func TestTailnetIP(t *testing.T) {
	cases := []struct {
		ips  []string
		want string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"100.64.0.1"}, "100.64.0.1"},
		{[]string{"fd7a:115c:a1e0::1", "100.64.0.1"}, "100.64.0.1"},
		{[]string{"fd7a:115c:a1e0::1"}, "fd7a:115c:a1e0::1"},
	}
	for _, c := range cases {
		if got := tailnetIP(c.ips); got != c.want {
			t.Errorf("tailnetIP(%v) = %q, want %q", c.ips, got, c.want)
		}
	}
}
