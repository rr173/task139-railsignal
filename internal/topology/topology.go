// Package topology models the yard as a directed graph of nodes and track
// sections, enforces acyclicity, and expands a route request (origin signal
// -> terminal section) into the concrete interlocking elements: the ordered
// route sections, the switches along the path with their required directions,
// the flank-protection switches, and the approach section.
package topology

import (
	"fmt"

	"task139-railsignal/internal/model"
)

// Graph is the mutable yard topology. It stores sections by id and by node,
// and points by id and by their heel node so route expansion can walk from
// the origin signal's entry node to the terminal section.
type Graph struct {
	nodes    map[string]*model.Node
	sections map[string]*model.TrackSection
	// byNode maps a node id to the list of sections touching it.
	byNode map[string][]string
	points map[string]*model.Point
	// pointAtNode maps heel-node -> point (a node holds at most one point).
	pointAtNode map[string]*model.Point
	signals     map[string]*model.Signal
}

// New constructs an empty yard graph.
func New() *Graph {
	return &Graph{
		nodes:       map[string]*model.Node{},
		sections:    map[string]*model.TrackSection{},
		byNode:      map[string][]string{},
		points:      map[string]*model.Point{},
		pointAtNode: map[string]*model.Point{},
		signals:     map[string]*model.Signal{},
	}
}

// AddNode inserts a node. Duplicate id is rejected.
func (g *Graph) AddNode(n *model.Node) error {
	if n.ID == "" {
		return model.Invariantf("node id is empty")
	}
	if _, ok := g.nodes[n.ID]; ok {
		return model.Conflictf("node %s already exists", n.ID)
	}
	if n.Code == "" {
		return model.Invariantf("node code is empty")
	}
	cp := *n
	g.nodes[n.ID] = &cp
	return nil
}

// AddSection inserts a directed section and records adjacency. It rejects:
// empty fields, duplicate id, unknown endpoints, and cycles.
func (g *Graph) AddSection(s *model.TrackSection) error {
	if s.ID == "" || s.Code == "" {
		return model.Invariantf("section id/code is empty")
	}
	if s.LengthM <= 0 {
		return model.Invariantf("section %s length must be > 0", s.ID)
	}
	if s.Kind != model.KindTrack && s.Kind != model.KindBlock && s.Kind != model.KindLadder {
		return model.Invariantf("section %s unknown kind %q", s.ID, s.Kind)
	}
	if _, ok := g.sections[s.ID]; ok {
		return model.Conflictf("section %s already exists", s.ID)
	}
	if _, ok := g.nodes[s.FromNodeID]; !ok {
		return model.NotFoundf("from-node %s not found", s.FromNodeID)
	}
	if _, ok := g.nodes[s.ToNodeID]; !ok {
		return model.NotFoundf("to-node %s not found", s.ToNodeID)
	}
	if s.FromNodeID == s.ToNodeID {
		return model.Conflictf("section %s is a self-loop", s.ID)
	}
	// cycle check: adding this from->to edge must not let `to` reach `from`.
	if g.reachable(s.ToNodeID, s.FromNodeID) {
		return model.Conflictf("section %s would form a cycle", s.ID)
	}
	cp := *s
	g.sections[s.ID] = &cp
	g.byNode[s.FromNodeID] = append(g.byNode[s.FromNodeID], s.ID)
	g.byNode[s.ToNodeID] = append(g.byNode[s.ToNodeID], s.ID)
	return nil
}

// AddPoint inserts a switch anchored on a node. The heel/normal/reverse
// sections must exist and the node must exist. A node holds at most one
// point (single-switch-per-node yard model).
func (g *Graph) AddPoint(p *model.Point) error {
	if p.ID == "" || p.Code == "" {
		return model.Invariantf("point id/code is empty")
	}
	if _, ok := g.points[p.ID]; ok {
		return model.Conflictf("point %s already exists", p.ID)
	}
	if _, ok := g.nodes[p.NodeID]; !ok {
		return model.NotFoundf("point node %s not found", p.NodeID)
	}
	if _, ok := g.sections[p.HeelSectionID]; !ok {
		return model.NotFoundf("heel section %s not found", p.HeelSectionID)
	}
	if _, ok := g.sections[p.NormalSectionID]; !ok {
		return model.NotFoundf("normal section %s not found", p.NormalSectionID)
	}
	if _, ok := g.sections[p.ReverseSectionID]; !ok {
		return model.NotFoundf("reverse section %s not found", p.ReverseSectionID)
	}
	if p.HeelSectionID == p.NormalSectionID || p.HeelSectionID == p.ReverseSectionID || p.NormalSectionID == p.ReverseSectionID {
		return model.Invariantf("point %s leg sections must be distinct", p.ID)
	}
	if p.Direction != model.DirNormal && p.Direction != model.DirReverse {
		return model.Invariantf("point %s invalid direction %q", p.ID, p.Direction)
	}
	if p.MaxMoveSeconds <= 0 {
		return model.Invariantf("point %s max_move_seconds must be > 0", p.ID)
	}
	if _, exists := g.pointAtNode[p.NodeID]; exists {
		return model.Conflictf("node %s already hosts a point", p.NodeID)
	}
	cp := *p
	// default status
	if cp.Status == "" {
		cp.Status = model.PointInPosition
	}
	g.points[p.ID] = &cp
	g.pointAtNode[p.NodeID] = &cp
	return nil
}

// AddSignal inserts a signal. Its entry node and guard section must exist.
func (g *Graph) AddSignal(s *model.Signal) error {
	if s.ID == "" || s.Code == "" {
		return model.Invariantf("signal id/code is empty")
	}
	if _, ok := g.signals[s.ID]; ok {
		return model.Conflictf("signal %s already exists", s.ID)
	}
	if _, ok := g.nodes[s.EntryNodeID]; !ok {
		return model.NotFoundf("signal entry node %s not found", s.EntryNodeID)
	}
	if _, ok := g.sections[s.GuardSectionID]; !ok {
		return model.NotFoundf("guard section %s not found", s.GuardSectionID)
	}
	cp := *s
	if cp.Aspect == "" {
		cp.Aspect = model.AspectRed
	}
	if cp.Status == "" {
		cp.Status = model.SignalSetRed
	}
	g.signals[s.ID] = &cp
	return nil
}

// reachable reports whether dst is reachable from src by walking existing
// sections as undirected edges. This is used for cycle detection.
func (g *Graph) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	visited := map[string]bool{}
	stack := []string{src}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == dst {
			return true
		}
		if visited[cur] {
			continue
		}
		visited[cur] = true
		for _, sid := range g.byNode[cur] {
			s := g.sections[sid]
			var other string
			if s.FromNodeID == cur {
				other = s.ToNodeID
			} else {
				other = s.FromNodeID
			}
			if !visited[other] {
				stack = append(stack, other)
			}
		}
	}
	return false
}

// Node returns a node by id.
func (g *Graph) Node(id string) (*model.Node, bool) {
	n, ok := g.nodes[id]
	return n, ok
}

// Section returns a section by id.
func (g *Graph) Section(id string) (*model.TrackSection, bool) {
	s, ok := g.sections[id]
	return s, ok
}

// Point returns a point by id.
func (g *Graph) Point(id string) (*model.Point, bool) {
	p, ok := g.points[id]
	return p, ok
}

// PointOnNode returns the point anchored on a node, if any.
func (g *Graph) PointOnNode(nodeID string) (*model.Point, bool) {
	p, ok := g.pointAtNode[nodeID]
	return p, ok
}

// Signal returns a signal by id.
func (g *Graph) Signal(id string) (*model.Signal, bool) {
	s, ok := g.signals[id]
	return s, ok
}

// Sections returns a snapshot of all sections (order unspecified).
func (g *Graph) Sections() []*model.TrackSection {
	out := make([]*model.TrackSection, 0, len(g.sections))
	for _, s := range g.sections {
		out = append(out, s)
	}
	return out
}

// Nodes returns a snapshot of all nodes.
func (g *Graph) Nodes() []*model.Node {
	out := make([]*model.Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	return out
}

// Points returns a snapshot of all points.
func (g *Graph) Points() []*model.Point {
	out := make([]*model.Point, 0, len(g.points))
	for _, p := range g.points {
		out = append(out, p)
	}
	return out
}

// Signals returns a snapshot of all signals.
func (g *Graph) Signals() []*model.Signal {
	out := make([]*model.Signal, 0, len(g.signals))
	for _, s := range g.signals {
		out = append(out, s)
	}
	return out
}

// SectionsAtNode returns sections touching a node.
func (g *Graph) SectionsAtNode(nodeID string) []string {
	return g.byNode[nodeID]
}

// OtherEnd returns the node id at the far end of section sid from nodeID.
func (g *Graph) OtherEnd(sid, nodeID string) (string, error) {
	s, ok := g.sections[sid]
	if !ok {
		return "", fmt.Errorf("section %s not found", sid)
	}
	if s.FromNodeID == nodeID {
		return s.ToNodeID, nil
	}
	if s.ToNodeID == nodeID {
		return s.FromNodeID, nil
	}
	return "", fmt.Errorf("section %s does not touch node %s", sid, nodeID)
}

// Reset clears all topology state (used by /layout/reset and tests).
func (g *Graph) Reset() {
	g.nodes = map[string]*model.Node{}
	g.sections = map[string]*model.TrackSection{}
	g.byNode = map[string][]string{}
	g.points = map[string]*model.Point{}
	g.pointAtNode = map[string]*model.Point{}
	g.signals = map[string]*model.Signal{}
}

// RemoveNode deletes a node only if no section or point references it.
func (g *Graph) RemoveNode(id string) error {
	if _, ok := g.nodes[id]; !ok {
		return nil
	}
	for _, sid := range g.byNode[id] {
		if _, ok := g.sections[sid]; ok {
			return model.Conflictf("node %s still referenced by section %s", id, sid)
		}
	}
	if _, ok := g.pointAtNode[id]; ok {
		return model.Conflictf("node %s still hosts a point", id)
	}
	delete(g.nodes, id)
	delete(g.byNode, id)
	return nil
}

// RemoveSection deletes a section and drops it from node adjacency. It
// refuses if a point references the section as a leg.
func (g *Graph) RemoveSection(id string) error {
	s, ok := g.sections[id]
	if !ok {
		return nil
	}
	for _, p := range g.points {
		if p.HeelSectionID == id || p.NormalSectionID == id || p.ReverseSectionID == id {
			return model.Conflictf("section %s still referenced by point %s", id, p.ID)
		}
	}
	delete(g.sections, id)
	g.byNode[s.FromNodeID] = removeStr(g.byNode[s.FromNodeID], id)
	g.byNode[s.ToNodeID] = removeStr(g.byNode[s.ToNodeID], id)
	return nil
}

// RemovePoint deletes a switch from the graph.
func (g *Graph) RemovePoint(id string) error {
	p, ok := g.points[id]
	if !ok {
		return nil
	}
	delete(g.points, id)
	delete(g.pointAtNode, p.NodeID)
	return nil
}

// RemoveSignal deletes a signal from the graph.
func (g *Graph) RemoveSignal(id string) error {
	delete(g.signals, id)
	return nil
}

func removeStr(xs []string, x string) []string {
	out := xs[:0]
	for _, v := range xs {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}
