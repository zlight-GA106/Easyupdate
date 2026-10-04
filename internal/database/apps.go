package database

import (
	"context"
	"database/sql"
	"errors"
)

var ErrConflict = errors.New("already exists or has associated releases")

const appColumns = `id,name,package_name,description,icon_path,created_at,updated_at`

type scanner interface{ Scan(...any) error }

func scanApp(row scanner) (App, error) {
	var a App
	err := row.Scan(&a.ID, &a.Name, &a.PackageName, &a.Description, &a.IconPath, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}
func (s *Store) App(ctx context.Context, id int64) (App, error) {
	return scanApp(s.DB.QueryRowContext(ctx, `SELECT `+appColumns+` FROM apps WHERE id=?`, id))
}
func (s *Store) AppByPackage(ctx context.Context, name string) (App, error) {
	return scanApp(s.DB.QueryRowContext(ctx, `SELECT `+appColumns+` FROM apps WHERE package_name=?`, name))
}
func (s *Store) Apps(ctx context.Context) ([]App, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id,a.name,a.package_name,a.description,a.icon_path,a.created_at,a.updated_at,
 COALESCE((SELECT version_name FROM releases WHERE app_id=a.id AND published=1 ORDER BY version_code DESC LIMIT 1),''),
 (SELECT COUNT(*) FROM releases WHERE app_id=a.id) FROM apps a ORDER BY a.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []App{}
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.Name, &a.PackageName, &a.Description, &a.IconPath, &a.CreatedAt, &a.UpdatedAt, &a.LatestVersion, &a.ReleaseCount); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}
func (s *Store) SaveApp(ctx context.Context, a App) (int64, error) {
	var exists int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM apps WHERE package_name=? AND id<>?`, a.PackageName, a.ID).Scan(&exists)
	if err != nil {
		return 0, err
	}
	if exists > 0 {
		return 0, ErrConflict
	}
	now := Now()
	if a.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO apps(name,package_name,description,created_at,updated_at) VALUES(?,?,?,?,?)`, a.Name, a.PackageName, a.Description, now, now)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	// A package identity stays stable once an APK has been added.
	old, err := s.App(ctx, a.ID)
	if err != nil {
		return 0, err
	}
	if old.PackageName != a.PackageName {
		if err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM releases WHERE app_id=?`, a.ID).Scan(&exists); err != nil {
			return 0, err
		}
		if exists > 0 {
			return 0, ErrConflict
		}
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE apps SET name=?,package_name=?,description=?,updated_at=? WHERE id=?`, a.Name, a.PackageName, a.Description, now, a.ID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, sql.ErrNoRows
	}
	return a.ID, nil
}
func (s *Store) DeleteApp(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM apps WHERE id=?`, id).Scan(&exists); err != nil {
		return err
	}
	// Releases use ON DELETE RESTRICT. Remove them in the same transaction;
	// deleting the app then cascades its devices and GitHub source.
	if _, err = tx.ExecContext(ctx, `DELETE FROM releases WHERE app_id=?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM apps WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}
