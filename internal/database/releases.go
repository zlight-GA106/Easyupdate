package database

import (
	"context"
	"database/sql"
)

const releaseColumns = `r.id,r.app_id,r.version_name,r.version_code,r.release_notes,r.apk_filename,r.apk_path,r.apk_size,r.apk_sha256,r.mandatory,r.published,r.created_at,COALESCE(r.published_at,''),COALESCE(r.github_release_id,0),r.github_tag,a.name,a.package_name`

func scanRelease(row scanner) (Release, error) {
	var r Release
	err := row.Scan(&r.ID, &r.AppID, &r.VersionName, &r.VersionCode, &r.ReleaseNotes, &r.APKFilename, &r.APKPath, &r.APKSize, &r.APKSHA256, &r.Mandatory, &r.Published, &r.CreatedAt, &r.PublishedAt, &r.GitHubReleaseID, &r.GitHubTag, &r.AppName, &r.PackageName)
	return r, err
}
func (s *Store) Release(ctx context.Context, id int64) (Release, error) {
	return scanRelease(s.DB.QueryRowContext(ctx, `SELECT `+releaseColumns+` FROM releases r JOIN apps a ON a.id=r.app_id WHERE r.id=?`, id))
}
func (s *Store) ReleaseByCode(ctx context.Context, appID, code int64) (Release, error) {
	return scanRelease(s.DB.QueryRowContext(ctx, `SELECT `+releaseColumns+` FROM releases r JOIN apps a ON a.id=r.app_id WHERE r.app_id=? AND r.version_code=?`, appID, code))
}
func (s *Store) Latest(ctx context.Context, appID int64) (Release, error) {
	return scanRelease(s.DB.QueryRowContext(ctx, `SELECT `+releaseColumns+` FROM releases r JOIN apps a ON a.id=r.app_id WHERE r.app_id=? AND r.published=1 ORDER BY r.version_code DESC LIMIT 1`, appID))
}
func (s *Store) Releases(ctx context.Context, appID int64) ([]Release, error) {
	query := `SELECT ` + releaseColumns + ` FROM releases r JOIN apps a ON a.id=r.app_id`
	args := []any{}
	if appID > 0 {
		query += ` WHERE r.app_id=? ORDER BY r.version_code DESC`
		args = append(args, appID)
	} else {
		query += ` ORDER BY r.created_at DESC LIMIT 8`
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Release{}
	for rows.Next() {
		r, e := scanRelease(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, r)
	}
	return items, rows.Err()
}
func (s *Store) CreateRelease(ctx context.Context, r Release) (int64, error) {
	var exists int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM releases WHERE app_id=? AND version_code=?`, r.AppID, r.VersionCode).Scan(&exists); err != nil {
		return 0, err
	}
	if exists > 0 {
		return 0, ErrConflict
	}
	var githubID any
	if r.GitHubReleaseID > 0 {
		githubID = r.GitHubReleaseID
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO releases(app_id,version_name,version_code,release_notes,apk_filename,apk_path,apk_size,apk_sha256,mandatory,created_at,github_release_id,github_tag) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, r.AppID, r.VersionName, r.VersionCode, r.ReleaseNotes, r.APKFilename, r.APKPath, r.APKSize, r.APKSHA256, r.Mandatory, Now(), githubID, r.GitHubTag)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
func (s *Store) EditRelease(ctx context.Context, id int64, notes string, mandatory bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE releases SET release_notes=?,mandatory=? WHERE id=?`, notes, mandatory, id)
	return err
}
func (s *Store) Publish(ctx context.Context, id int64, published bool) error {
	var at any
	if published {
		at = Now()
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE releases SET published=?,published_at=CASE WHEN ?=1 THEN COALESCE(published_at,?) ELSE published_at END WHERE id=?`, published, published, at, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Store) DeleteRelease(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM releases WHERE id=?`, id)
	return err
}
