package database

import "context"

func scanAnnouncement(row scanner) (Announcement, error) {
	var a Announcement
	err := row.Scan(&a.ID, &a.Title, &a.Content, &a.Published, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}
func (s *Store) Announcement(ctx context.Context, id int64) (Announcement, error) {
	return scanAnnouncement(s.DB.QueryRowContext(ctx, `SELECT id,title,content,published,created_at,updated_at FROM announcements WHERE id=?`, id))
}
func (s *Store) Announcements(ctx context.Context, public bool) ([]Announcement, error) {
	query := `SELECT id,title,content,published,created_at,updated_at FROM announcements`
	if public {
		query += ` WHERE published=1`
	}
	query += ` ORDER BY created_at DESC,id DESC`
	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Announcement{}
	for rows.Next() {
		a, e := scanAnnouncement(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, a)
	}
	return items, rows.Err()
}
func (s *Store) SaveAnnouncement(ctx context.Context, a Announcement) (int64, error) {
	now := Now()
	if a.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO announcements(title,content,published,created_at,updated_at) VALUES(?,?,?,?,?)`, a.Title, a.Content, a.Published, now, now)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE announcements SET title=?,content=?,published=?,updated_at=? WHERE id=?`, a.Title, a.Content, a.Published, now, a.ID)
	return a.ID, err
}
func (s *Store) PublishAnnouncement(ctx context.Context, id int64, pub bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE announcements SET published=?,updated_at=? WHERE id=?`, pub, Now(), id)
	return err
}
func (s *Store) DeleteAnnouncement(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM announcements WHERE id=?`, id)
	return err
}
