package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cooked/scraper"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	if err := initSchema(db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func initSchema(db *sql.DB) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS recipes (
	id TEXT PRIMARY KEY,
	url TEXT NOT NULL,
	payload TEXT NOT NULL,
	scraped_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_recipes_url ON recipes(url);
`
	_, err := db.Exec(ddl)
	return err
}

func (s *Store) SaveRecipe(ctx context.Context, id, url string, payload scraper.RecipeJSON) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal recipe: %w", err)
	}
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO recipes(id, url, payload, scraped_at)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET url=excluded.url, payload=excluded.payload, scraped_at=excluded.scraped_at`,
		id, url, string(data), time.Now().UTC(),
	)
	return err
}

func (s *Store) GetRecipe(ctx context.Context, id string) (scraper.RecipeJSON, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM recipes WHERE id = ?`, id).Scan(&payload)
	if err != nil {
		return scraper.RecipeJSON{}, err
	}
	return decodeRecipeJSON(payload)
}

func (s *Store) AllRecipes(ctx context.Context) ([]scraper.RecipeJSON, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM recipes ORDER BY scraped_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []scraper.RecipeJSON
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		rec, err := decodeRecipeJSON(payload)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func decodeRecipeJSON(raw string) (scraper.RecipeJSON, error) {
	if raw == "" {
		return scraper.RecipeJSON{}, errors.New("empty payload")
	}
	var rec scraper.RecipeJSON
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return scraper.RecipeJSON{}, err
	}
	return rec, nil
}
