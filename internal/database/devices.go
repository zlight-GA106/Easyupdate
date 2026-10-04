package database

import "context"

func (s *Store) Heartbeat(ctx context.Context, d Device) error {
	now := Now()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO devices(device_id,app_id,version_name,version_code,first_seen,last_seen) VALUES(?,?,?,?,?,?) ON CONFLICT(device_id,app_id) DO UPDATE SET version_name=excluded.version_name,version_code=excluded.version_code,last_seen=excluded.last_seen`, d.DeviceID, d.AppID, d.VersionName, d.VersionCode, now, now)
	return err
}
func (s *Store) Devices(ctx context.Context, appID int64, limit, offset int) ([]Device, error) {
	query := `SELECT d.id,d.device_id,d.app_id,a.name,d.version_name,d.version_code,d.first_seen,d.last_seen FROM devices d JOIN apps a ON a.id=d.app_id`
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
		var d Device
		if err = rows.Scan(&d.ID, &d.DeviceID, &d.AppID, &d.AppName, &d.VersionName, &d.VersionCode, &d.FirstSeen, &d.LastSeen); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}
