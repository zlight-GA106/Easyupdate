package database

import (
	"context"
	"strings"
)

const deviceColumns = `d.id,d.device_id,d.app_id,a.name,d.version_name,d.version_code,d.first_seen,d.last_seen`

func scanDevice(row scanner) (Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.DeviceID, &d.AppID, &d.AppName, &d.VersionName, &d.VersionCode, &d.FirstSeen, &d.LastSeen)
	return d, err
}

func (s *Store) Device(ctx context.Context, id int64) (Device, error) {
	return scanDevice(s.DB.QueryRowContext(ctx, `SELECT `+deviceColumns+` FROM devices d JOIN apps a ON a.id=d.app_id WHERE d.id=?`, id))
}

// SaveDevice changes the record without impersonating a client heartbeat.
// Manual creation sets FirstSeen; LastSeen stays empty until a real heartbeat.
func (s *Store) SaveDevice(ctx context.Context, d Device) (int64, error) {
	d.DeviceID = strings.ToLower(strings.TrimSpace(d.DeviceID))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM apps WHERE id=?`, d.AppID).Scan(&exists); err != nil {
		return 0, err
	}
	if d.ID != 0 {
		if err = tx.QueryRowContext(ctx, `SELECT 1 FROM devices WHERE id=?`, d.ID).Scan(&exists); err != nil {
			return 0, err
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE device_id=? AND app_id=? AND id<>?`, d.DeviceID, d.AppID, d.ID).Scan(&exists); err != nil {
		return 0, err
	}
	if exists > 0 {
		return 0, ErrConflict
	}
	id := d.ID
	if id == 0 {
		now := Now()
		result, err := tx.ExecContext(ctx, `INSERT INTO devices(device_id,app_id,version_name,version_code,first_seen,last_seen) VALUES(?,?,?,?,?,?)`, d.DeviceID, d.AppID, d.VersionName, d.VersionCode, now, "")
		if err != nil {
			return 0, err
		}
		id, err = result.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE devices SET device_id=?,app_id=?,version_name=?,version_code=? WHERE id=?`, d.DeviceID, d.AppID, d.VersionName, d.VersionCode, id); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// The confirmation remains part of the DELETE condition so an identity edited
// after the confirmation page was opened cannot be deleted under its old UUID.
func (s *Store) DeleteDevice(ctx context.Context, id int64, confirmedDeviceID string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM devices WHERE id=? AND device_id=?`, id, confirmedDeviceID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var exists int
	if err = s.DB.QueryRowContext(ctx, `SELECT 1 FROM devices WHERE id=?`, id).Scan(&exists); err != nil {
		return err
	}
	return ErrConflict
}

func (s *Store) Heartbeat(ctx context.Context, d Device) error {
	now := Now()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO devices(device_id,app_id,version_name,version_code,first_seen,last_seen) VALUES(?,?,?,?,?,?) ON CONFLICT(device_id,app_id) DO UPDATE SET version_name=excluded.version_name,version_code=excluded.version_code,last_seen=excluded.last_seen`, d.DeviceID, d.AppID, d.VersionName, d.VersionCode, now, now)
	return err
}
func (s *Store) Devices(ctx context.Context, appID int64, limit, offset int) ([]Device, error) {
	query := `SELECT ` + deviceColumns + ` FROM devices d JOIN apps a ON a.id=d.app_id`
	args := []any{}
	if appID > 0 {
		query += ` WHERE d.app_id=?`
		args = append(args, appID)
	}
	query += ` ORDER BY d.last_seen DESC,d.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}
