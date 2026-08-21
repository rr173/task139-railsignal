package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"task139-railsignal/internal/model"
)

// This file exposes the DBTX-variant methods (the "*Tx" / "*DB" family) so the
// service layer can compose several writes inside one WithTx transaction.
// With SetMaxOpenConns(1), a transaction holds the only connection, so any
// read/write inside the closure MUST use these DBTX variants — not the
// connection-bound methods — or it will deadlock.

// NodeTx inserts/updates a node within a transaction.
func (s *Store) NodeTx(ctx context.Context, db DBTX, n *model.Node) error { return s.insertNode(ctx, db, n) }

// SectionTx upserts a section within a transaction (insert or update runtime fields).
func (s *Store) SectionTx(ctx context.Context, db DBTX, sec *model.TrackSection) error {
	// try update first; if no row affected, insert
	res, err := db.ExecContext(ctx, `UPDATE sections SET occupancy_count=?, locked_by_route=? WHERE id=?`, sec.OccupancyCnt, sec.LockedByRoute, sec.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	return s.insertSection(ctx, db, sec)
}

// PointTx upserts a point within a transaction.
func (s *Store) PointTx(ctx context.Context, db DBTX, p *model.Point) error {
	prot, err := json.Marshal(p.ProtectSections)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE points SET direction=?,status=?,target_direction=?,move_start_time=?,move_deadline=?,locked_by_route=?,bypassed=?,protect_sections=? WHERE id=?`,
		string(p.Direction), string(p.Status), string(p.TargetDirection), p.MoveStartTime, p.MoveDeadline, p.LockedByRoute, boolToInt(p.Bypassed), string(prot), p.ID)
	if err != nil {
		// fall back to insert (point may not yet exist)
		return s.insertPoint(ctx, db, p)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.insertPoint(ctx, db, p)
	}
	return nil
}

// SignalTx upserts a signal within a transaction.
func (s *Store) SignalTx(ctx context.Context, db DBTX, sig *model.Signal) error {
	res, err := db.ExecContext(ctx, `UPDATE signals SET aspect=?,status=?,route_id=? WHERE id=?`, string(sig.Aspect), string(sig.Status), sig.RouteID, sig.ID)
	if err != nil {
		return s.insertSignal(ctx, db, sig)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.insertSignal(ctx, db, sig)
	}
	return nil
}

// RouteTx upserts a route within a transaction.
func (s *Store) RouteTx(ctx context.Context, db DBTX, r *model.Route) error {
	res, err := db.ExecContext(ctx, `UPDATE routes SET state=?,opened_at=?,cancel_deadline=?,released_count=?,conflict_detail=? WHERE id=?`,
		string(r.State), r.OpenedAt, r.CancelDeadline, r.ReleasedCount, "[]", r.ID)
	if err != nil {
		return s.insertRoute(ctx, db, r)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.insertRoute(ctx, db, r)
	}
	// ensure runtime fields fully synced
	return s.updateRoute(ctx, db, r)
}

// EventTx appends an event within a transaction.
func (s *Store) EventTx(ctx context.Context, db DBTX, e *model.Event) (int64, error) {
	return s.appendEvent(ctx, db, e)
}

// NextSeqTx returns the next event sequence within a transaction (sees prior
// inserts in the same tx).
func (s *Store) NextSeqTx(ctx context.Context, db DBTX) (int64, error) {
	var seq sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM events`).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return seq.Int64, nil
}

// SectionTxGet loads a section within a transaction.
func (s *Store) SectionTxGet(ctx context.Context, db DBTX, id string) (*model.TrackSection, error) {
	return s.getSection(ctx, db, id)
}

// PointTxGet loads a point within a transaction.
func (s *Store) PointTxGet(ctx context.Context, db DBTX, id string) (*model.Point, error) {
	return s.getPoint(ctx, db, id)
}

// SignalTxGet loads a signal within a transaction.
func (s *Store) SignalTxGet(ctx context.Context, db DBTX, id string) (*model.Signal, error) {
	return s.getSignal(ctx, db, id)
}

// RouteTxGet loads a route within a transaction.
func (s *Store) RouteTxGet(ctx context.Context, db DBTX, id string) (*model.Route, error) {
	return s.getRoute(ctx, db, id)
}

// ---------------------------------------------------------------------------
// public DBTX-variant inserters (used by the service inside its own WriteTx).
// Each upserts: if the row exists it updates; otherwise it inserts.
// ---------------------------------------------------------------------------

// InsertNodeTx inserts a node (DBTX-variant).
func (s *Store) InsertNodeTx(ctx context.Context, db DBTX, n *model.Node) error {
	return s.insertNode(ctx, db, n)
}

// InsertSectionTx inserts a section (DBTX-variant).
func (s *Store) InsertSectionTx(ctx context.Context, db DBTX, sec *model.TrackSection) error {
	return s.insertSection(ctx, db, sec)
}

// InsertPointTx inserts a point (DBTX-variant).
func (s *Store) InsertPointTx(ctx context.Context, db DBTX, p *model.Point) error {
	return s.insertPoint(ctx, db, p)
}

// InsertSignalTx inserts a signal (DBTX-variant).
func (s *Store) InsertSignalTx(ctx context.Context, db DBTX, sig *model.Signal) error {
	return s.insertSignal(ctx, db, sig)
}

// InsertRouteTx inserts a route (DBTX-variant).
func (s *Store) InsertRouteTx(ctx context.Context, db DBTX, r *model.Route) error {
	return s.insertRoute(ctx, db, r)
}

// UpdateSectionTx updates a section's runtime fields (DBTX-variant).
func (s *Store) UpdateSectionTx(ctx context.Context, db DBTX, sec *model.TrackSection) error {
	return s.updateSection(ctx, db, sec)
}

// UpdatePointTx updates a point's runtime fields (DBTX-variant).
func (s *Store) UpdatePointTx(ctx context.Context, db DBTX, p *model.Point) error {
	return s.updatePoint(ctx, db, p)
}

// UpdateSignalTx updates a signal's runtime fields (DBTX-variant).
func (s *Store) UpdateSignalTx(ctx context.Context, db DBTX, sig *model.Signal) error {
	return s.updateSignal(ctx, db, sig)
}

// UpdateRouteTx updates a route's runtime fields (DBTX-variant).
func (s *Store) UpdateRouteTx(ctx context.Context, db DBTX, r *model.Route) error {
	return s.updateRoute(ctx, db, r)
}

// SetMetaTx persists a meta key/value within a transaction.
func (s *Store) SetMetaTx(ctx context.Context, db DBTX, key, value string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// GetMetaTx reads a meta key within a transaction.
func (s *Store) GetMetaTx(ctx context.Context, db DBTX, key string) (string, error) {
	var v string
	err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// ResetDBTx truncates all entity tables within a transaction.
func (s *Store) ResetDBTx(ctx context.Context) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM events; DELETE FROM routes; DELETE FROM signals; DELETE FROM points; DELETE FROM sections; DELETE FROM nodes; DELETE FROM meta;`)
		return err
	})
}
