// Package model defines the domain types, enums, and errors for the
// railway signal interlocking engine. It is the shared vocabulary used by
// topology, interlocking, point, signal, route, store, recovery and service.
package model

// Node is an endpoint shared by one or more track sections. Points (switches)
// are anchored on a node. Nodes form the vertices of the directed yard graph.
type Node struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

// TrackSectionKind classifies a track section.
type TrackSectionKind string

const (
	KindTrack  TrackSectionKind = "track"  // 股道 (station track)
	KindBlock  TrackSectionKind = "block" // 区间 (block section between stations)
	KindLadder TrackSectionKind = "ladder" // 衔接段 (ladder track connecting points)
)

// TrackSection is a directed segment between two nodes. Trains may travel in
// either direction; the direction is only a layout convention.
type TrackSection struct {
	ID            string           `json:"id"`
	Code          string           `json:"code"`
	LengthM       int              `json:"length_m"` // length in meters (fixed-point integer)
	Kind          TrackSectionKind `json:"kind"`
	FromNodeID    string           `json:"from_node_id"`
	ToNodeID      string           `json:"to_node_id"`
	OccupancyCnt  int              `json:"occupancy_count"`   // >=1 means occupied /压车
	LockedByRoute string           `json:"locked_by_route"`   // route id locking this section, "" if free
}

// Occupied reports whether the section is currently occupied.
func (s TrackSection) Occupied() bool { return s.OccupancyCnt > 0 }

// Locked reports whether the section is locked by an in-progress route.
func (s TrackSection) Locked() bool { return s.LockedByRoute != "" }

// PointDirection is the facing direction of a switch: N (normal/定位) or
// R (reverse/反位).
type PointDirection string

const (
	DirNormal  PointDirection = "N"
	DirReverse PointDirection = "R"
)

// Opposite returns the opposite direction.
func (d PointDirection) Opposite() PointDirection {
	if d == DirNormal {
		return DirReverse
	}
	return DirNormal
}

// PointStatus is the lifecycle status of a switch machine.
type PointStatus string

const (
	PointFree                    PointStatus = "FREE"                    // idle, no command outstanding
	PointMoving                  PointStatus = "MOVING"                  // command issued, awaiting detection
	PointInPosition              PointStatus = "IN_POSITION"             // detected at target direction
	PointOutOfCorrespondence     PointStatus = "OUT_OF_CORRESPONDENCE" // move timeout, position unconfirmed
	PointFault                   PointStatus = "FAULT"                   // mechanical fault (manual)
)

// Point is a railway switch anchored on a node. The heel/common section is
// where trains converge/diverge; normal and reverse sections are the two legs.
type Point struct {
	ID             string        `json:"id"`
	Code           string        `json:"code"`
	NodeID         string        `json:"node_id"`          // heel node
	HeelSectionID  string        `json:"heel_section_id"`  // common/heel leg
	NormalSectionID string       `json:"normal_section_id"`
	ReverseSectionID string      `json:"reverse_section_id"`
	Direction      PointDirection `json:"direction"`       // currently set direction
	Status         PointStatus   `json:"status"`
	TargetDirection PointDirection `json:"target_direction"` // goal of an in-progress move
	MoveStartTime  int           `json:"move_start_time"`  // sim clock when move began
	MoveDeadline   int           `json:"move_deadline"`     // sim clock by which detection must arrive
	MaxMoveSeconds int           `json:"max_move_seconds"`  // configured max move time
	LockedByRoute  string        `json:"locked_by_route"`   // route id locking this point, "" if free
	Bypassed       bool          `json:"bypassed"`           // manually bypassed (degraded mode)
	ProtectSections []string     `json:"protect_sections"`   // anti-squeeze sections: must be free to move
}

// IsMovable reports whether the point can currently receive a move command
// (not faulted, not already moving to the same target).
func (p Point) IsMovable() bool {
	return p.Status != PointFault && p.Status != PointOutOfCorrespondence
}

// AtTarget reports whether the point sits detected at the given direction.
func (p Point) AtTarget(dir PointDirection) bool {
	return p.Status == PointInPosition && p.Direction == dir
}

// SignalAspect is the displayed colour of a signal.
type SignalAspect string

const (
	AspectRed          SignalAspect = "RED"           // stop
	AspectGreen       SignalAspect = "GREEN"          // clear, straight route
	AspectYellow      SignalAspect = "YELLOW"         // caution, diverging at single point
	AspectDoubleYellow SignalAspect = "DOUBLE_YELLOW" // diverging route over multiple points
)

// SignalStatus is the operational status of a signal head.
type SignalStatus string

const (
	SignalDark      SignalStatus = "DARK"       // no power / out of use
	SignalSetRed    SignalStatus = "SET_RED"    // forced red / cancelling
	SignalClearable SignalStatus = "CLEARABLE"  // cleared for a route, may be cancelled
)

// Signal guards the entrance to a track section.
type Signal struct {
	ID            string       `json:"id"`
	Code          string       `json:"code"`
	EntryNodeID   string       `json:"entry_node_id"` // node where route begins
	GuardSectionID string      `json:"guard_section_id"` // section this signal protects
	Aspect        SignalAspect `json:"aspect"`
	Status        SignalStatus `json:"status"`
	RouteID       string       `json:"route_id"` // route this signal is cleared for, "" if none
}

// RouteState is the lifecycle state of an interlocked route.
type RouteState string

const (
	RoutePending        RouteState = "PENDING"         // request submitted, not yet processed
	RoutePointsMoving   RouteState = "POINTS_MOVING"   // switches moving to target direction
	RouteLocked         RouteState = "LOCKED"          // all switches detected, signal cleared
	RouteTrainComing    RouteState = "TRAIN_COMING"     // train occupied approach section
	RouteTrainInRoute   RouteState = "TRAIN_IN_ROUTE"   // train occupied first route section
	RouteReleasing      RouteState = "RELEASING"       // train clearing sections, route unlocking
	RouteReleased       RouteState = "RELEASED"         // fully unlocked, points/sections reusable
	RouteConflict       RouteState = "CONFLICT"         // interlocking check failed
	RouteCancelPending  RouteState = "CANCEL_PENDING"  // cancel requested but route occupied
	RouteCancelled      RouteState = "CANCELLED"       // cancelled (was unoccupied)
	RouteFailed         RouteState = "FAILED"           // switch fault during setup
)

// IsTerminal reports whether the state is a final absorbing state.
func (s RouteState) IsTerminal() bool {
	return s == RouteReleased || s == RouteCancelled
}

// IsActive reports whether the route still holds interlocking resources
// (locks points and sections). Terminal, cancelled, conflict, failed and
// pending (not yet locked) routes hold nothing.
func (s RouteState) IsActive() bool {
	switch s {
	case RoutePointsMoving,
		RouteLocked,
		RouteTrainComing,
		RouteTrainInRoute,
		RouteReleasing,
		RouteCancelPending:
		return true
	default:
		return false
	}
}

// DivergingRoute reports whether the route requires any reverse switch, i.e.
// leaves the straight path. This drives the signal aspect.
func (r *Route) DivergingRoute() bool {
	for _, pr := range r.PointsRequired {
		if pr.Direction == DirReverse {
			return true
		}
	}
	return false
}

// RequiresPoint reports whether the route (path or flank) references a point.
func (r *Route) RequiresPoint(pointID string) bool {
	for _, pr := range r.PointsRequired {
		if pr.PointID == pointID {
			return true
		}
	}
	for _, pr := range r.FlankProtection {
		if pr.PointID == pointID {
			return true
		}
	}
	return false
}

// DirectionForPoint returns the direction a route requires a point to be in,
// or "" if the route does not require that point. Path requirements take
// priority over flank protection.
func (r *Route) DirectionForPoint(pointID string) PointDirection {
	for _, pr := range r.PointsRequired {
		if pr.PointID == pointID {
			return pr.Direction
		}
	}
	for _, pr := range r.FlankProtection {
		if pr.PointID == pointID {
			return pr.Direction
		}
	}
	return ""
}

// PathSet returns the set of path section ids for quick lookup.
func (r *Route) PathSet() map[string]bool {
	m := make(map[string]bool, len(r.PathSections))
	for _, s := range r.PathSections {
		m[s] = true
	}
	return m
}

// PointRequirement is a switch and the direction a route needs it set to.
type PointRequirement struct {
	PointID   string        `json:"point_id"`
	Direction PointDirection `json:"direction"`
}

// ConflictItem describes one reason an interlocking check failed.
type ConflictItem struct {
	Kind    string `json:"kind"`    // SECTION_LOCKED / SECTION_OCCUPIED / POINT_CONFLICT / FLANK_CONFLICT / OPPOSING_ROUTE / TERMINAL_OCCUPIED
	RefID   string `json:"ref_id"`  // section/point/route id at fault
	Detail  string `json:"detail"`
}

// Route is an interlocked path from an origin signal to a terminal section.
type Route struct {
	ID                 string             `json:"id"`
	Code               string             `json:"code"`
	OriginSignalID     string             `json:"origin_signal_id"`
	TerminalSectionID  string             `json:"terminal_section_id"`
	TerminalKind       TrackSectionKind   `json:"terminal_kind"`
	State              RouteState         `json:"state"`
	PathSections       []string           `json:"path_sections"`        // ordered route sections (excluding approach)
	PointsRequired    []PointRequirement `json:"points_required"`      // switches and required directions
	FlankProtection   []PointRequirement `json:"flank_protection"`     // flank protection switches
	ApproachSectionID string             `json:"approach_section_id"`  // approach section for TRAIN_COMING
	OpenedAt          int                `json:"opened_at"`            // sim clock when signal cleared
	CancelDeadline    int                `json:"cancel_deadline"`      // CANCEL_PENDING timed release deadline
	ReleasedCount     int                `json:"released_count"`       // sections unlocked so far (route-release progress)
	ConflictDetail    []ConflictItem     `json:"conflict_detail"`
	TransitSec        int                `json:"transit_sec"`          // expected train transit time across the path (s)
}

// EventKind enumerates authoritative write events persisted for recovery/audit.
type EventKind string

const (
	EventNodeAdd        EventKind = "NODE_ADD"
	EventSectionAdd     EventKind = "SECTION_ADD"
	EventPointAdd       EventKind = "POINT_ADD"
	EventSignalAdd      EventKind = "SIGNAL_ADD"
	EventRouteRequest   EventKind = "ROUTE_REQUEST"
	EventRouteConflict  EventKind = "ROUTE_CONFLICT"
	EventPointMove      EventKind = "POINT_MOVE"
	EventPointDetect    EventKind = "POINT_DETECT"
	EventPointTimeout   EventKind = "POINT_TIMEOUT"
	EventPointBypass    EventKind = "POINT_BYPASS"
	EventPointClearBypass EventKind = "POINT_CLEAR_BYPASS"
	EventSignalClear    EventKind = "SIGNAL_CLEAR"
	EventSignalSetRed   EventKind = "SIGNAL_SET_RED"
	EventOccupancy      EventKind = "OCCUPANCY"
	EventClearance      EventKind = "CLEARANCE"
	EventRouteCancel    EventKind = "ROUTE_CANCEL"
	EventClockAdvance   EventKind = "CLOCK_ADVANCE"
	EventRouteRelease   EventKind = "ROUTE_RELEASE"
)

// Event is an authoritative persisted write used for audit and deterministic
// recovery replay.
type Event struct {
	ID      int64    `json:"id"`
	Seq     int64    `json:"seq"`
	Kind    EventKind `json:"kind"`
	Payload string   `json:"payload"` // JSON string of the event-specific body
	Clock   int      `json:"clock"`
}
