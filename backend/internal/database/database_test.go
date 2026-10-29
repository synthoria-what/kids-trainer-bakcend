package database

import (
	"path/filepath"
	"testing"
)

func TestPaletteSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "palette.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO users(login,password_hash,role,name) VALUES('palette','hash','child','Test'); INSERT INTO user_preferences(user_id,palette_index) VALUES(1,3)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var palette int
	if err = db.QueryRow(`SELECT palette_index FROM user_preferences WHERE user_id=1`).Scan(&palette); err != nil {
		t.Fatal(err)
	}
	if palette != 3 {
		t.Fatalf("palette lost: %d", palette)
	}
}

func TestDatasetTasksAreSeededAndNotDuplicated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dataset.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE title LIKE '% — ситуация %'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 50 {
		t.Fatalf("expected 50 seeded dataset tasks, got %d", count)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE title LIKE '% — ситуация %'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != count {
		t.Fatalf("dataset tasks were duplicated: %d -> %d", count, after)
	}
}

func TestTaskEditsSurviveRestartAndMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err = db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE tasks SET title='Новое название',correct_choice=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var title string
	var correct, after int
	if err = db.QueryRow(`SELECT title,correct_choice FROM tasks WHERE id=1`).Scan(&title, &correct); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if title != "Новое название" || correct != 0 || before != after {
		t.Fatalf("edits lost or tasks reseeded: %q %d %d/%d", title, correct, before, after)
	}
}
