package store

import (
	"context"
	"time"
)

type TemplateRating struct {
	ID           string    `json:"id"`
	TemplateName string    `json:"template_name"`
	UserID       string    `json:"user_id"`
	Rating       int       `json:"rating"`
	ReviewText   string    `json:"review_text"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Store) UpsertTemplateRating(ctx context.Context, templateName, userID string, rating int, review string) (*TemplateRating, error) {
	r := &TemplateRating{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO template_rating (template_name, user_id, rating, review_text)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (template_name, user_id) DO UPDATE SET rating=$3, review_text=$4
		 RETURNING id, template_name, user_id, rating, review_text, created_at`,
		templateName, userID, rating, review,
	).Scan(&r.ID, &r.TemplateName, &r.UserID, &r.Rating, &r.ReviewText, &r.CreatedAt)
	return r, err
}

func (s *Store) ListTemplateRatings(ctx context.Context, templateName string) ([]TemplateRating, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, template_name, user_id, rating, review_text, created_at
		 FROM template_rating WHERE template_name = $1 ORDER BY created_at DESC`, templateName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ratings []TemplateRating
	for rows.Next() {
		var r TemplateRating
		if err := rows.Scan(&r.ID, &r.TemplateName, &r.UserID, &r.Rating, &r.ReviewText, &r.CreatedAt); err != nil {
			return nil, err
		}
		ratings = append(ratings, r)
	}
	return ratings, nil
}

func (s *Store) IncrementTemplateInstallCount(ctx context.Context, templateName string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO template_install_count (template_name, install_count) VALUES ($1, 1)
		 ON CONFLICT (template_name) DO UPDATE SET install_count = template_install_count.install_count + 1`,
		templateName)
	return err
}

func (s *Store) GetTemplateInstallCount(ctx context.Context, templateName string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(install_count, 0) FROM template_install_count WHERE template_name = $1`, templateName).Scan(&count)
	if err != nil {
		return 0, nil
	}
	return count, nil
}

func (s *Store) PopularTemplates(ctx context.Context, limit int) ([]struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT template_name, install_count FROM template_install_count ORDER BY install_count DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	for rows.Next() {
		var r struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}
		if err := rows.Scan(&r.Name, &r.Count); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

func (s *Store) TopRatedTemplates(ctx context.Context, limit int) ([]struct {
	Name      string  `json:"name"`
	AvgRating float64 `json:"avg_rating"`
	Count     int     `json:"count"`
}, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT template_name, AVG(rating)::numeric(3,2), COUNT(*)
		 FROM template_rating GROUP BY template_name
		 HAVING COUNT(*) >= 1 ORDER BY AVG(rating) DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []struct {
		Name      string  `json:"name"`
		AvgRating float64 `json:"avg_rating"`
		Count     int     `json:"count"`
	}
	for rows.Next() {
		var r struct {
			Name      string  `json:"name"`
			AvgRating float64 `json:"avg_rating"`
			Count     int     `json:"count"`
		}
		if err := rows.Scan(&r.Name, &r.AvgRating, &r.Count); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}
