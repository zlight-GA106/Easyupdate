package database

import "context"

func (s *Store) GitHubSource(ctx context.Context, appID int64) (GitHubSource, error) {
	var g GitHubSource
	err := s.DB.QueryRowContext(ctx, `SELECT id,app_id,owner,repo,enabled,asset_pattern,COALESCE(last_checked_at,''),COALESCE(last_release_id,0) FROM github_sources WHERE app_id=?`, appID).Scan(&g.ID, &g.AppID, &g.Owner, &g.Repo, &g.Enabled, &g.AssetPattern, &g.LastCheckedAt, &g.LastReleaseID)
	return g, err
}
func (s *Store) SaveGitHubSource(ctx context.Context, g GitHubSource) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO github_sources(app_id,owner,repo,enabled,asset_pattern) VALUES(?,?,?,?,?) ON CONFLICT(app_id) DO UPDATE SET owner=excluded.owner,repo=excluded.repo,enabled=excluded.enabled,asset_pattern=excluded.asset_pattern,last_checked_at=CASE WHEN owner<>excluded.owner OR repo<>excluded.repo THEN NULL ELSE last_checked_at END,last_release_id=CASE WHEN owner<>excluded.owner OR repo<>excluded.repo THEN NULL ELSE last_release_id END`, g.AppID, g.Owner, g.Repo, g.Enabled, g.AssetPattern)
	return err
}
func (s *Store) GitHubChecked(ctx context.Context, appID, releaseID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE github_sources SET last_checked_at=?,last_release_id=CASE WHEN ?>0 THEN ? ELSE last_release_id END WHERE app_id=?`, Now(), releaseID, releaseID, appID)
	return err
}
func (s *Store) GitHubImported(ctx context.Context, appID, id int64) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM releases WHERE app_id=? AND github_release_id=?`, appID, id).Scan(&count)
	return count > 0, err
}
