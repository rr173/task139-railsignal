package topology

import (
	"errors"
	"fmt"

	"task139-railsignal/internal/model"
)

// ExpandedRoute is the result of expanding a route request: the ordered
// sections the train will traverse (path), the switches that must be set
// (points_required), the flank-protection switches, the approach section
// feeding the origin signal, and whether the route runs straight or diverges
// (affects the displayed aspect).
type ExpandedRoute struct {
	PathSections     []string
	PointsRequired   []model.PointRequirement
	FlankProtection  []model.PointRequirement
	ApproachSectionID string
	Diverging        bool // true if any required point is reverse
}

// Expand walks from the origin signal's entry node through the yard until it
// reaches the terminal section, choosing the point leg (normal/reverse) that
// keeps the walk progressing. It collects:
//   - the path sections in order,
//   - each point along the path with the direction matching the leg used,
//   - flank-protection points guarding the path sections (a point that
//     branches off the path's sections but is not itself on the path must be
//     set to protect, i.e. away from the path).
//
// The approach section is the section immediately upstream of the signal's
// entry node (the one not on the path). The terminal section must not be
// occupied when the route is set up (checked by interlocking, not here).
func (g *Graph) Expand(originSignalID, terminalSectionID string) (*ExpandedRoute, error) {
	sig, ok := g.signals[originSignalID]
	if !ok {
		return nil, model.NotFoundf("signal %s not found", originSignalID)
	}
	if _, ok := g.sections[terminalSectionID]; !ok {
		return nil, model.NotFoundf("terminal section %s not found", terminalSectionID)
	}
	// approach = a section touching the entry node that is NOT the guard section.
	approach := ""
	for _, sid := range g.byNode[sig.EntryNodeID] {
		if sid == sig.GuardSectionID {
			continue
		}
		approach = sid
		break
	}
	if approach == "" {
		return nil, model.Conflictf("signal %s has no approach section (entry node %s has no upstream section)", sig.ID, sig.EntryNodeID)
	}

	// walk from entry node into the guard section, towards the terminal.
	path := []string{}
	var pointsReq []model.PointRequirement
	visited := map[string]bool{}
	curNode := sig.EntryNodeID
	curSection := sig.GuardSectionID
	for {
		if curSection == terminalSectionID {
			path = append(path, curSection)
			break
		}
		if visited[curSection] {
			return nil, model.Conflictf("route expansion looped at section %s", curSection)
		}
		visited[curSection] = true
		path = append(path, curSection)
		// advance to the far node of curSection
		farNode, err := g.OtherEnd(curSection, curNode)
		if err != nil {
			return nil, fmt.Errorf("expand: %w", err)
		}
		// if a point sits on farNode, decide which leg to take towards the terminal
		pt, hasPt := g.pointAtNode[farNode]
		nextSection := ""
		if hasPt {
			// the heel of this point is the section we just came along only if
			// the point's heel section is curSection; otherwise we are arriving
			// from a leg and must use the heel to continue.
			var legTaken model.PointDirection
			var otherLeg string
			if pt.HeelSectionID == curSection {
				// arriving at heel: choose the leg that leads toward terminal.
				// Block the OTHER leg so the reachability check does not fold back
				// through the heel into the other leg (which would make both legs
				// appear to reach either terminal).
				if reachesTerminal(g, pt.NormalSectionID, terminalSectionID, copyBlocked(visited, pt.ReverseSectionID)) {
					legTaken = model.DirNormal
					nextSection = pt.NormalSectionID
					otherLeg = pt.ReverseSectionID
				} else if reachesTerminal(g, pt.ReverseSectionID, terminalSectionID, copyBlocked(visited, pt.NormalSectionID)) {
					legTaken = model.DirReverse
					nextSection = pt.ReverseSectionID
					otherLeg = pt.NormalSectionID
				} else {
					return nil, model.Conflictf("route expansion: point %s has no leg reaching terminal %s", pt.ID, terminalSectionID)
				}
				// record the point with the direction of the leg actually taken,
				// so a diverging route requires the reverse (not the normal) leg.
				pointsReq = append(pointsReq, model.PointRequirement{PointID: pt.ID, Direction: legTaken})
				_ = otherLeg
			} else {
				// arriving from a leg: continue via the heel.
				nextSection = pt.HeelSectionID
				// the requirement for this point was already recorded when we
				// chose the leg on the prior pass.
			}
		} else {
			// no point: pick the adjacent section (other than the one we came from)
			// that leads toward the terminal.
			for _, sid := range g.byNode[farNode] {
				if sid == curSection {
					continue
				}
				if reachesTerminal(g, sid, terminalSectionID, visited) {
					nextSection = sid
					break
				}
			}
		}
		if nextSection == "" {
			return nil, model.Conflictf("route expansion dead-ended at node %s (no path to %s)", farNode, terminalSectionID)
		}
		curNode = farNode
		curSection = nextSection
	}

	// flank protection: any point whose heel section is ON the path but whose
	// point itself is not part of points_required must be set away from the
	// path so a movement from a side leg cannot foul the route.
	pathSet := map[string]bool{}
	for _, p := range path {
		pathSet[p] = true
	}
	reqSet := map[string]bool{}
	for _, pr := range pointsReq {
		reqSet[pr.PointID] = true
	}
	var flank []model.PointRequirement
	for _, pt := range g.points {
		if reqSet[pt.ID] {
			continue
		}
		if pathSet[pt.HeelSectionID] {
			// this point's heel is on the path; protect by setting it to the
			// leg that is NOT on the path.
			if pathSet[pt.NormalSectionID] && !pathSet[pt.ReverseSectionID] {
				flank = append(flank, model.PointRequirement{PointID: pt.ID, Direction: model.DirReverse})
			} else if pathSet[pt.ReverseSectionID] && !pathSet[pt.NormalSectionID] {
				flank = append(flank, model.PointRequirement{PointID: pt.ID, Direction: model.DirNormal})
			} else if !pathSet[pt.NormalSectionID] && !pathSet[pt.ReverseSectionID] {
				// neither leg on path: protect either way; default reverse
				flank = append(flank, model.PointRequirement{PointID: pt.ID, Direction: model.DirReverse})
			}
		}
	}

	diverging := false
	for _, pr := range pointsReq {
		if pr.Direction == model.DirReverse {
			diverging = true
			break
		}
	}
	return &ExpandedRoute{
		PathSections:      path,
		PointsRequired:    pointsReq,
		FlankProtection:   flank,
		ApproachSectionID: approach,
		Diverging:         diverging,
	}, nil
}

// reachesTerminal reports whether walking from sid (as undirected edges) can
// reach terminalSectionID without crossing sections already in `visited`.
// `visited` prevents backtracking into the path already built.
func reachesTerminal(g *Graph, sid, terminalSectionID string, visited map[string]bool) bool {
	if sid == terminalSectionID {
		return true
	}
	if visited[sid] {
		return false
	}
	type frame struct {
		section string
	}
	seen := map[string]bool{sid: true}
	stack := []frame{{section: sid}}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur.section == terminalSectionID {
			return true
		}
		s, ok := g.sections[cur.section]
		if !ok {
			continue
		}
		for _, n := range []string{s.FromNodeID, s.ToNodeID} {
			for _, adj := range g.byNode[n] {
				if adj == cur.section || seen[adj] || visited[adj] {
					continue
				}
				seen[adj] = true
				stack = append(stack, frame{section: adj})
			}
		}
	}
	return false
}

// ErrNoPath is returned when no route can be expanded between signal and terminal.
var ErrNoPath = errors.New("no route path between signal and terminal")

// copyBlocked returns a copy of visited with sid added, so the caller can
// block a leg without mutating the shared visited set.
func copyBlocked(visited map[string]bool, sid string) map[string]bool {
	out := make(map[string]bool, len(visited)+1)
	for k, v := range visited {
		out[k] = v
	}
	out[sid] = true
	return out
}

func init() {
	_ = fmt.Sprintf // keep fmt import referenced if unused in future edits
	_ = ErrNoPath
}
