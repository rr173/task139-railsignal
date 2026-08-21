package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"task139-railsignal/internal/model"
)

// ---------------------------------------------------------------------------
// nodes
// ---------------------------------------------------------------------------

// InsertNode persists a node.
func (s *Store) InsertNode(ctx context.Context, n *model.Node) error {
	return s.insertNode(ctx, s.db, n)
}
func (s *Store) insertNode(ctx context.Context, db DBTX, n *model.Node) error {
	_, err := db.ExecContext(ctx, `INSERT INTO nodes(id,code) VALUES(?,?)`, n.ID, n.Code)
	return err
}

// ListNodes returns all nodes.
func (s *Store) ListNodes(ctx context.Context) ([]*model.Node, error) {
	return s.listNodes(ctx, s.db)
}
func (s *Store) listNodes(ctx context.Context, db DBTX) ([]*model.Node, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, code FROM nodes ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Node
	for rows.Next() {
		var n model.Node
		if err := rows.Scan(&n.ID, &n.Code); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// sections
// ---------------------------------------------------------------------------

// InsertSection persists a track section.
func (s *Store) InsertSection(ctx context.Context, sec *model.TrackSection) error {
	return s.insertSection(ctx, s.db, sec)
}
func (s *Store) insertSection(ctx context.Context, db DBTX, sec *model.TrackSection) error {
	_, err := db.ExecContext(ctx, `INSERT INTO sections(id,code,length_m,kind,from_node,to_node,occupancy_count,locked_by_route) VALUES(?,?,?,?,?,?,?,?)`,
		sec.ID, sec.Code, sec.LengthM, string(sec.Kind), sec.FromNodeID, sec.ToNodeID, sec.OccupancyCnt, sec.LockedByRoute)
	return err
}

// UpdateSection writes back runtime fields (occupancy, lock).
func (s *Store) UpdateSection(ctx context.Context, sec *model.TrackSection) error {
	return s.updateSection(ctx, s.db, sec)
}
func (s *Store) updateSection(ctx context.Context, db DBTX, sec *model.TrackSection) error {
	_, err := db.ExecContext(ctx, `UPDATE sections SET occupancy_count=?, locked_by_route=? WHERE id=?`,
		sec.OccupancyCnt, sec.LockedByRoute, sec.ID)
	return err
}

// GetSection loads a section by id.
func (s *Store) GetSection(ctx context.Context, id string) (*model.TrackSection, error) {
	return s.getSection(ctx, s.db, id)
}
func (s *Store) getSection(ctx context.Context, db DBTX, id string) (*model.TrackSection, error) {
	var sec model.TrackSection
	var kind string
	err := db.QueryRowContext(ctx, `SELECT id,code,length_m,kind,from_node,to_node,occupancy_count,locked_by_route FROM sections WHERE id=?`, id).
		Scan(&sec.ID, &sec.Code, &sec.LengthM, &kind, &sec.FromNodeID, &sec.ToNodeID, &sec.OccupancyCnt, &sec.LockedByRoute)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("section %s: %w", id, err)
		}
		return nil, err
	}
	sec.Kind = model.TrackSectionKind(kind)
	return &sec, nil
}

// ListSections returns all sections.
func (s *Store) ListSections(ctx context.Context) ([]*model.TrackSection, error) {
	return s.listSections(ctx, s.db)
}
func (s *Store) listSections(ctx context.Context, db DBTX) ([]*model.TrackSection, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,code,length_m,kind,from_node,to_node,occupancy_count,locked_by_route FROM sections ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.TrackSection
	for rows.Next() {
		var sec model.TrackSection
		var kind string
		if err := rows.Scan(&sec.ID, &sec.Code, &sec.LengthM, &kind, &sec.FromNodeID, &sec.ToNodeID, &sec.OccupancyCnt, &sec.LockedByRoute); err != nil {
			return nil, err
		}
		sec.Kind = model.TrackSectionKind(kind)
		out = append(out, &sec)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// points
// ---------------------------------------------------------------------------

// InsertPoint persists a switch.
func (s *Store) InsertPoint(ctx context.Context, p *model.Point) error {
	return s.insertPoint(ctx, s.db, p)
}
func (s *Store) insertPoint(ctx context.Context, db DBTX, p *model.Point) error {
	prot, err := json.Marshal(p.ProtectSections)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO points(id,code,node_id,heel_section,normal_section,reverse_section,direction,status,target_direction,move_start_time,move_deadline,max_move_seconds,locked_by_route,bypassed,protect_sections) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Code, p.NodeID, p.HeelSectionID, p.NormalSectionID, p.ReverseSectionID,
		string(p.Direction), string(p.Status), string(p.TargetDirection),
		p.MoveStartTime, p.MoveDeadline, p.MaxMoveSeconds, p.LockedByRoute, boolToInt(p.Bypassed), string(prot))
	return err
}

// UpdatePoint writes back runtime fields.
func (s *Store) UpdatePoint(ctx context.Context, p *model.Point) error {
	return s.updatePoint(ctx, s.db, p)
}
func (s *Store) updatePoint(ctx context.Context, db DBTX, p *model.Point) error {
	prot, err := json.Marshal(p.ProtectSections)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE points SET direction=?,status=?,target_direction=?,move_start_time=?,move_deadline=?,locked_by_route=?,bypassed=?,protect_sections=? WHERE id=?`,
		string(p.Direction), string(p.Status), string(p.TargetDirection),
		p.MoveStartTime, p.MoveDeadline, p.LockedByRoute, boolToInt(p.Bypassed), string(prot), p.ID)
	return err
}

// GetPoint loads a switch by id.
func (s *Store) GetPoint(ctx context.Context, id string) (*model.Point, error) {
	return s.getPoint(ctx, s.db, id)
}
func (s *Store) getPoint(ctx context.Context, db DBTX, id string) (*model.Point, error) {
	var p model.Point
	var direction, status, target, prot string
	var bypassed int
	err := db.QueryRowContext(ctx, `SELECT id,code,node_id,heel_section,normal_section,reverse_section,direction,status,target_direction,move_start_time,move_deadline,max_move_seconds,locked_by_route,bypassed,protect_sections FROM points WHERE id=?`, id).
		Scan(&p.ID, &p.Code, &p.NodeID, &p.HeelSectionID, &p.NormalSectionID, &p.ReverseSectionID,
			&direction, &status, &target, &p.MoveStartTime, &p.MoveDeadline, &p.MaxMoveSeconds, &p.LockedByRoute, &bypassed, &prot)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("point %s: %w", id, err)
		}
		return nil, err
	}
	p.Direction = model.PointDirection(direction)
	p.Status = model.PointStatus(status)
	p.TargetDirection = model.PointDirection(target)
	p.Bypassed = bypassed != 0
	if err := json.Unmarshal([]byte(prot), &p.ProtectSections); err != nil {
		return nil, fmt.Errorf("decode protect_sections: %w", err)
	}
	return &p, nil
}

// ListPoints returns all switches.
func (s *Store) ListPoints(ctx context.Context) ([]*model.Point, error) {
	return s.listPoints(ctx, s.db)
}
func (s *Store) listPoints(ctx context.Context, db DBTX) ([]*model.Point, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,code,node_id,heel_section,normal_section,reverse_section,direction,status,target_direction,move_start_time,move_deadline,max_move_seconds,locked_by_route,bypassed,protect_sections FROM points ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Point
	for rows.Next() {
		var p model.Point
		var direction, status, target, prot string
		var bypassed int
		if err := rows.Scan(&p.ID, &p.Code, &p.NodeID, &p.HeelSectionID, &p.NormalSectionID, &p.ReverseSectionID,
			&direction, &status, &target, &p.MoveStartTime, &p.MoveDeadline, &p.MaxMoveSeconds, &p.LockedByRoute, &bypassed, &prot); err != nil {
			return nil, err
		}
		p.Direction = model.PointDirection(direction)
		p.Status = model.PointStatus(status)
		p.TargetDirection = model.PointDirection(target)
		p.Bypassed = bypassed != 0
		if err := json.Unmarshal([]byte(prot), &p.ProtectSections); err != nil {
			return nil, fmt.Errorf("decode protect_sections: %w", err)
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// signals
// ---------------------------------------------------------------------------

// InsertSignal persists a signal.
func (s *Store) InsertSignal(ctx context.Context, sig *model.Signal) error {
	return s.insertSignal(ctx, s.db, sig)
}
func (s *Store) insertSignal(ctx context.Context, db DBTX, sig *model.Signal) error {
	_, err := db.ExecContext(ctx, `INSERT INTO signals(id,code,entry_node,guard_section,aspect,status,route_id) VALUES(?,?,?,?,?,?,?)`,
		sig.ID, sig.Code, sig.EntryNodeID, sig.GuardSectionID, string(sig.Aspect), string(sig.Status), sig.RouteID)
	return err
}

// UpdateSignal writes back runtime fields.
func (s *Store) UpdateSignal(ctx context.Context, sig *model.Signal) error {
	return s.updateSignal(ctx, s.db, sig)
}
func (s *Store) updateSignal(ctx context.Context, db DBTX, sig *model.Signal) error {
	_, err := db.ExecContext(ctx, `UPDATE signals SET aspect=?,status=?,route_id=? WHERE id=?`,
		string(sig.Aspect), string(sig.Status), sig.RouteID, sig.ID)
	return err
}

// GetSignal loads a signal by id.
func (s *Store) GetSignal(ctx context.Context, id string) (*model.Signal, error) {
	return s.getSignal(ctx, s.db, id)
}
func (s *Store) getSignal(ctx context.Context, db DBTX, id string) (*model.Signal, error) {
	var sig model.Signal
	var aspect, status string
	err := db.QueryRowContext(ctx, `SELECT id,code,entry_node,guard_section,aspect,status,route_id FROM signals WHERE id=?`, id).
		Scan(&sig.ID, &sig.Code, &sig.EntryNodeID, &sig.GuardSectionID, &aspect, &status, &sig.RouteID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("signal %s: %w", id, err)
		}
		return nil, err
	}
	sig.Aspect = model.SignalAspect(aspect)
	sig.Status = model.SignalStatus(status)
	return &sig, nil
}

// ListSignals returns all signals.
func (s *Store) ListSignals(ctx context.Context) ([]*model.Signal, error) {
	return s.listSignals(ctx, s.db)
}
func (s *Store) listSignals(ctx context.Context, db DBTX) ([]*model.Signal, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,code,entry_node,guard_section,aspect,status,route_id FROM signals ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Signal
	for rows.Next() {
		var sig model.Signal
		var aspect, status string
		if err := rows.Scan(&sig.ID, &sig.Code, &sig.EntryNodeID, &sig.GuardSectionID, &aspect, &status, &sig.RouteID); err != nil {
			return nil, err
		}
		sig.Aspect = model.SignalAspect(aspect)
		sig.Status = model.SignalStatus(status)
		out = append(out, &sig)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// routes
// ---------------------------------------------------------------------------

// InsertRoute persists a route.
func (s *Store) InsertRoute(ctx context.Context, r *model.Route) error {
	return s.insertRoute(ctx, s.db, r)
}
func (s *Store) insertRoute(ctx context.Context, db DBTX, r *model.Route) error {
	path, _ := json.Marshal(r.PathSections)
	pts, _ := json.Marshal(r.PointsRequired)
	flank, _ := json.Marshal(r.FlankProtection)
	conflict, _ := json.Marshal(r.ConflictDetail)
	_, err := db.ExecContext(ctx, `INSERT INTO routes(id,code,origin_signal,terminal_section,terminal_kind,state,path_sections,points_required,flank_protection,approach_section,opened_at,cancel_deadline,released_count,conflict_detail,diverging,transit_sec) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Code, r.OriginSignalID, r.TerminalSectionID, string(r.TerminalKind), string(r.State),
		string(path), string(pts), string(flank), r.ApproachSectionID, r.OpenedAt, r.CancelDeadline, r.ReleasedCount, string(conflict), boolToInt(r.DivergingRoute()), r.TransitSec)
	return err
}

// UpdateRoute writes back runtime fields.
func (s *Store) UpdateRoute(ctx context.Context, r *model.Route) error {
	return s.updateRoute(ctx, s.db, r)
}
func (s *Store) updateRoute(ctx context.Context, db DBTX, r *model.Route) error {
	path, _ := json.Marshal(r.PathSections)
	pts, _ := json.Marshal(r.PointsRequired)
	flank, _ := json.Marshal(r.FlankProtection)
	conflict, _ := json.Marshal(r.ConflictDetail)
	_, err := db.ExecContext(ctx, `UPDATE routes SET state=?,path_sections=?,points_required=?,flank_protection=?,approach_section=?,opened_at=?,cancel_deadline=?,released_count=?,conflict_detail=?,diverging=?,transit_sec=? WHERE id=?`,
		string(r.State), string(path), string(pts), string(flank), r.ApproachSectionID, r.OpenedAt, r.CancelDeadline, r.ReleasedCount, string(conflict), boolToInt(r.DivergingRoute()), r.TransitSec, r.ID)
	return err
}

// GetRoute loads a route by id.
func (s *Store) GetRoute(ctx context.Context, id string) (*model.Route, error) {
	return s.getRoute(ctx, s.db, id)
}
func (s *Store) getRoute(ctx context.Context, db DBTX, id string) (*model.Route, error) {
	var r model.Route
	var state, termKind, path, pts, flank, conflict string
	var diverging int
	err := db.QueryRowContext(ctx, `SELECT id,code,origin_signal,terminal_section,terminal_kind,state,path_sections,points_required,flank_protection,approach_section,opened_at,cancel_deadline,released_count,conflict_detail,diverging,transit_sec FROM routes WHERE id=?`, id).
		Scan(&r.ID, &r.Code, &r.OriginSignalID, &r.TerminalSectionID, &termKind, &state, &path, &pts, &flank, &r.ApproachSectionID, &r.OpenedAt, &r.CancelDeadline, &r.ReleasedCount, &conflict, &diverging, &r.TransitSec)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("route %s: %w", id, err)
		}
		return nil, err
	}
	r.TerminalKind = model.TrackSectionKind(termKind)
	r.State = model.RouteState(state)
	if err := json.Unmarshal([]byte(path), &r.PathSections); err != nil {
		return nil, fmt.Errorf("decode path: %w", err)
	}
	if err := json.Unmarshal([]byte(pts), &r.PointsRequired); err != nil {
		return nil, fmt.Errorf("decode points: %w", err)
	}
	if err := json.Unmarshal([]byte(flank), &r.FlankProtection); err != nil {
		return nil, fmt.Errorf("decode flank: %w", err)
	}
	if err := json.Unmarshal([]byte(conflict), &r.ConflictDetail); err != nil {
		return nil, fmt.Errorf("decode conflict: %w", err)
	}
	return &r, nil
}

// ListRoutes returns all routes, optionally filtered by state ("" = all).
func (s *Store) ListRoutes(ctx context.Context, state string) ([]*model.Route, error) {
	return s.listRoutes(ctx, s.db, state)
}
func (s *Store) listRoutes(ctx context.Context, db DBTX, state string) ([]*model.Route, error) {
	var rows *sql.Rows
	var err error
	if state != "" {
		rows, err = db.QueryContext(ctx, `SELECT id,code,origin_signal,terminal_section,terminal_kind,state,path_sections,points_required,flank_protection,approach_section,opened_at,cancel_deadline,released_count,conflict_detail,diverging,transit_sec FROM routes WHERE state=? ORDER BY id`, state)
	} else {
		rows, err = db.QueryContext(ctx, `SELECT id,code,origin_signal,terminal_section,terminal_kind,state,path_sections,points_required,flank_protection,approach_section,opened_at,cancel_deadline,released_count,conflict_detail,diverging,transit_sec FROM routes ORDER BY id`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Route
	for rows.Next() {
		var r model.Route
		var st, termKind, path, pts, flank, conflict string
		var diverging int
		if err := rows.Scan(&r.ID, &r.Code, &r.OriginSignalID, &r.TerminalSectionID, &termKind, &st, &path, &pts, &flank, &r.ApproachSectionID, &r.OpenedAt, &r.CancelDeadline, &r.ReleasedCount, &conflict, &diverging, &r.TransitSec); err != nil {
			return nil, err
		}
		r.TerminalKind = model.TrackSectionKind(termKind)
		r.State = model.RouteState(st)
		if err := json.Unmarshal([]byte(path), &r.PathSections); err != nil {
			return nil, fmt.Errorf("decode path: %w", err)
		}
		if err := json.Unmarshal([]byte(pts), &r.PointsRequired); err != nil {
			return nil, fmt.Errorf("decode points: %w", err)
		}
		if err := json.Unmarshal([]byte(flank), &r.FlankProtection); err != nil {
			return nil, fmt.Errorf("decode flank: %w", err)
		}
		if err := json.Unmarshal([]byte(conflict), &r.ConflictDetail); err != nil {
			return nil, fmt.Errorf("decode conflict: %w", err)
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// DeleteRoute removes a route row (used on reset).
func (s *Store) DeleteRoute(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM routes WHERE id=?`, id)
	return err
}

// ---------------------------------------------------------------------------
// events + meta
// ---------------------------------------------------------------------------

// AppendEvent appends an authoritative event and returns its assigned id.
func (s *Store) AppendEvent(ctx context.Context, e *model.Event) (int64, error) {
	return s.appendEvent(ctx, s.db, e)
}
func (s *Store) appendEvent(ctx context.Context, db DBTX, e *model.Event) (int64, error) {
	res, err := db.ExecContext(ctx, `INSERT INTO events(seq,kind,payload,clock) VALUES(?,?,?,?)`,
		e.Seq, string(e.Kind), e.Payload, e.Clock)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// NextEventSeq returns the next event sequence number.
func (s *Store) NextEventSeq(ctx context.Context) (int64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM events`).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return seq.Int64, nil
}

// ListEvents returns events ordered by seq, paginated.
func (s *Store) ListEvents(ctx context.Context, limit, offset int) ([]*model.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,seq,kind,payload,clock FROM events ORDER BY seq LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Event
	for rows.Next() {
		var e model.Event
		var kind string
		if err := rows.Scan(&e.ID, &e.Seq, &kind, &e.Payload, &e.Clock); err != nil {
			return nil, err
		}
		e.Kind = model.EventKind(kind)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// SetMeta persists a key/value.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// GetMeta reads a key; returns "" and false if missing.
func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// ResetDB truncates all entity tables (smoke-test/debug only).
func (s *Store) ResetDB(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM events; DELETE FROM routes; DELETE FROM signals; DELETE FROM points; DELETE FROM sections; DELETE FROM nodes; DELETE FROM meta;`)
	return err
}

// boolToInt maps bool to integer for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
