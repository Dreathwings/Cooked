package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type RecipeStore struct {
	db          *sql.DB
	reseededDDL bool
}

func OpenRecipeStore(path string) (*RecipeStore, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &RecipeStore{db: db}
	if err := store.migrate(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *RecipeStore) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS recipes (
  id TEXT PRIMARY KEY,
  title TEXT,
  recipe_name TEXT,
  recipe_name_min TEXT,
  description TEXT,
  url TEXT,
  image TEXT,
  prep_time TEXT,
  difficulty TEXT,
  origin TEXT,
  tags TEXT,
  utensils TEXT,
  allergens TEXT,
  nutrition TEXT,
  ingredients TEXT,
  steps TEXT,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`
	if _, err := s.db.Exec(ddl); err != nil {
		return err
	}

	columns, err := listColumns(s.db, "recipes")
	if err != nil {
		return err
	}

	required := []string{"id", "title", "recipe_name", "recipe_name_min", "description", "url", "image", "prep_time", "difficulty", "origin", "tags", "utensils", "allergens", "nutrition", "ingredients", "steps", "updated_at"}
	missing := missingColumns(columns, required)
	if len(missing) == 0 {
		return nil
	}

	log.Printf("migration: detected missing columns in recipes table: %s; rebuilding table", strings.Join(missing, ", "))
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE recipes RENAME TO recipes_old`); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(ddl); err != nil {
		tx.Rollback()
		return err
	}

	var selectExprs []string
	for _, col := range required {
		if columns[col] {
			selectExprs = append(selectExprs, col)
		} else {
			selectExprs = append(selectExprs, fmt.Sprintf("'' AS %s", col))
		}
	}

	insertQuery := fmt.Sprintf("INSERT INTO recipes (%s) SELECT %s FROM recipes_old", strings.Join(required, ", "), strings.Join(selectExprs, ", "))
	if _, err := tx.Exec(insertQuery); err != nil {
		tx.Rollback()
		return err
	}

	if _, err := tx.Exec(`DROP TABLE recipes_old`); err != nil {
		tx.Rollback()
		return err
	}

	s.reseededDDL = true
	return tx.Commit()
}

func (s *RecipeStore) ReplaceAll(ctx context.Context, recipes []RecipeDocument) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM recipes"); err != nil {
		tx.Rollback()
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO recipes (id, title, recipe_name, recipe_name_min, description, url, image, prep_time, difficulty, origin, tags, utensils, allergens, nutrition, ingredients, steps, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for _, r := range recipes {
		tags, _ := json.Marshal(r.Tags)
		utensils, _ := json.Marshal(r.Utensils)
		allergens, _ := json.Marshal(r.Allergens)
		nutrition, _ := json.Marshal(r.Nutrition)
		ingredients, _ := json.Marshal(r.Ingredients1P)
		steps, _ := json.Marshal(r.Steps)

		if _, err := stmt.ExecContext(ctx, r.ID, r.Title, r.RecipeName, r.RecipeNameMin, r.Description, r.URL, r.Image, r.PrepTime, r.Difficulty, r.Origin, string(tags), string(utensils), string(allergens), string(nutrition), string(ingredients), string(steps), now); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *RecipeStore) ListRecipes(ctx context.Context) ([]RecipeDocument, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, title, recipe_name, recipe_name_min, description, url, image, prep_time, difficulty, origin, tags, utensils, allergens, nutrition, ingredients, steps
FROM recipes
ORDER BY recipe_name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RecipeDocument
	for rows.Next() {
		var r RecipeDocument
		var tags, utensils, allergens, nutrition, ingredients, steps string
		if err := rows.Scan(&r.ID, &r.Title, &r.RecipeName, &r.RecipeNameMin, &r.Description, &r.URL, &r.Image, &r.PrepTime, &r.Difficulty, &r.Origin, &tags, &utensils, &allergens, &nutrition, &ingredients, &steps); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tags), &r.Tags)
		_ = json.Unmarshal([]byte(utensils), &r.Utensils)
		_ = json.Unmarshal([]byte(allergens), &r.Allergens)
		_ = json.Unmarshal([]byte(nutrition), &r.Nutrition)
		_ = json.Unmarshal([]byte(ingredients), &r.Ingredients1P)
		_ = json.Unmarshal([]byte(steps), &r.Steps)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *RecipeStore) GetRecipe(ctx context.Context, id string) (RecipeDocument, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, title, recipe_name, recipe_name_min, description, url, image, prep_time, difficulty, origin, tags, utensils, allergens, nutrition, ingredients, steps
FROM recipes WHERE id = ?`, id)

	var r RecipeDocument
	var tags, utensils, allergens, nutrition, ingredients, steps string
	if err := row.Scan(&r.ID, &r.Title, &r.RecipeName, &r.RecipeNameMin, &r.Description, &r.URL, &r.Image, &r.PrepTime, &r.Difficulty, &r.Origin, &tags, &utensils, &allergens, &nutrition, &ingredients, &steps); err != nil {
		return RecipeDocument{}, err
	}
	_ = json.Unmarshal([]byte(tags), &r.Tags)
	_ = json.Unmarshal([]byte(utensils), &r.Utensils)
	_ = json.Unmarshal([]byte(allergens), &r.Allergens)
	_ = json.Unmarshal([]byte(nutrition), &r.Nutrition)
	_ = json.Unmarshal([]byte(ingredients), &r.Ingredients1P)
	_ = json.Unmarshal([]byte(steps), &r.Steps)
	return r, nil
}

func (s *RecipeStore) Close() error {
	if s == nil || s.db == nil {
		return errors.New("store not initialised")
	}
	return s.db.Close()
}

func (s *RecipeStore) NeedsReseed() bool {
	return s.reseededDDL
}

func listColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func missingColumns(existing map[string]bool, required []string) []string {
	var missing []string
	for _, col := range required {
		if !existing[col] {
			missing = append(missing, col)
		}
	}
	return missing
}
