package database

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed dataset_tasks.json
var datasetTasksJSON []byte

type datasetTask struct {
	Category    string   `json:"category"`
	Scene       string   `json:"scene"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Prompt      string   `json:"prompt"`
	Choices     []string `json:"choices"`
	Difficulty  string   `json:"difficulty"`
	AnswerMode  string   `json:"answer_mode"`
	SafetyTip   string   `json:"safety_tip"`
}

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// SQLite is used as the local/prototype store. A single connection keeps
	// PRAGMA state consistent across reads and writes.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS users (
 id INTEGER PRIMARY KEY AUTOINCREMENT, login TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('child','parent')), name TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS child_profiles (
 id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
 name TEXT NOT NULL, age INTEGER NOT NULL CHECK(age BETWEEN 6 AND 12), parent_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS user_preferences (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 palette_index INTEGER NOT NULL DEFAULT 0 CHECK(palette_index BETWEEN 0 AND 5)
);
CREATE TABLE IF NOT EXISTS invite_codes (
 id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 code_hash TEXT NOT NULL, expires_at DATETIME NOT NULL, used_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS tasks (
 id INTEGER PRIMARY KEY AUTOINCREMENT, category TEXT NOT NULL, scene TEXT NOT NULL, title TEXT NOT NULL,
 description TEXT NOT NULL, prompt TEXT NOT NULL, choices_json TEXT NOT NULL DEFAULT '[]', difficulty TEXT NOT NULL,
 answer_mode TEXT NOT NULL, safety_tip TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS task_attempts (
 id INTEGER PRIMARY KEY AUTOINCREMENT, child_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 task_id INTEGER NOT NULL REFERENCES tasks(id), answer TEXT NOT NULL, mode TEXT NOT NULL,
 ai_is_safe INTEGER, ai_summary TEXT NOT NULL DEFAULT '', ai_tip TEXT NOT NULL DEFAULT '', ai_is_demo INTEGER NOT NULL DEFAULT 0,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS daily_activity (
 id INTEGER PRIMARY KEY AUTOINCREMENT, child_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 activity_date TEXT NOT NULL, completed_count INTEGER NOT NULL DEFAULT 0,
 UNIQUE(child_id, activity_date)
);
CREATE INDEX IF NOT EXISTS idx_attempt_child_date ON task_attempts(child_id, created_at);
CREATE INDEX IF NOT EXISTS idx_activity_child_date ON daily_activity(child_id, activity_date);
`)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	// Versioned additive migration keeps existing installations and task edits intact.
	var columns int
	rows, err := db.Query(`PRAGMA table_info(tasks)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "correct_choice" {
			columns++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if columns == 0 {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN correct_choice INTEGER`); err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE tasks SET correct_choice=1 WHERE json_array_length(choices_json)>=2`); err != nil {
			return err
		}
	}
	var seeded int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&seeded); err != nil {
		return err
	}
	// Seed only on a new database or when the bundled task set is upgraded.
	// seedTasks checks titles, so existing admin edits are preserved.
	if seeded < 2 {
		if err := seedTasks(db); err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE tasks SET correct_choice=1 WHERE correct_choice IS NULL AND json_array_length(choices_json)>=2`); err != nil {
			return err
		}
		if _, err := db.Exec(`PRAGMA user_version=2`); err != nil {
			return err
		}
	}
	if err := seedDatasetTasks(db); err != nil {
		return err
	}
	return nil
}

func seedDatasetTasks(db *sql.DB) error {
	var tasks []datasetTask
	if err := json.Unmarshal(datasetTasksJSON, &tasks); err != nil {
		return fmt.Errorf("decode bundled dataset tasks: %w", err)
	}
	for _, task := range tasks {
		choices, err := json.Marshal(task.Choices)
		if err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO tasks(category,scene,title,description,prompt,choices_json,difficulty,answer_mode,safety_tip)
			SELECT ?,?,?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM tasks WHERE title=?)`,
			task.Category, task.Scene, task.Title, task.Description, task.Prompt, string(choices), task.Difficulty, task.AnswerMode, task.SafetyTip, task.Title); err != nil {
			return fmt.Errorf("seed dataset task %q: %w", task.Title, err)
		}
	}
	return nil
}

func seedTasks(db *sql.DB) error {
	for _, task := range []struct{ category, scene, title, description, prompt, choices, difficulty, mode, tip string }{
		{"city", "Ситуация в городе", "Незнакомец у подъезда", "Ты возвращаешься домой. Незнакомый человек просит открыть дверь подъезда и говорит, что забыл ключи.", "Как ты поступишь?", `["Открою дверь и пойду вместе","Не открою, отойду и позвоню взрослым","Спрошу его пароль"]`, "easy", "choices", "Не открывай дверь незнакомому человеку. Отойди туда, где есть люди, и свяжись со взрослым."},
		{"city", "Ситуация в городе", "Дорога к школе", "На пути к школе загорелся зелёный сигнал. Рядом нет машин, но велосипедист быстро приближается.", "Что важно сделать перед переходом?", `["Побежать через дорогу","Остановиться, посмотреть по сторонам и перейти спокойно","Закрыть глаза и идти"]`, "medium", "choices", "Остановись, посмотри по сторонам и переходи только когда все участники движения тебя пропускают."},
		{"home", "Безопасность дома", "Сообщение в интернете", "Тебе пишет незнакомый человек и просит отправить фотографию и адрес, обещая подарок.", "Как будет безопаснее?", `["Отправить только адрес","Не отвечать, сохранить сообщение и рассказать взрослому","Попросить подарок побольше"]`, "medium", "free", "Не отправляй незнакомцу адрес и фотографии. Покажи сообщение родителю."},
		{"countryside", "Ситуация за городом", "Один в лесу", "Во время прогулки ты заметил, что отстал от взрослых и не узнаёшь дорогу.", "Что нужно сделать?", `["Бежать куда угодно","Остаться на месте и позвонить взрослому","Прятаться от всех"]`, "hard", "free", "Останься на безопасном месте, чтобы тебя было легче найти, и позвони взрослому."},
		{"city", "Ситуация в городе", "Незнакомец предлагает подвезти", "Ты идёшь домой после кружка. У обочины останавливается машина, водитель говорит, что его прислали родители, потому что они задерживаются. Он знает твоё имя и предлагает подвезти. Телефон почти разрядился.", "Что ты сделаешь?", `[]`, "hard", "free", "Не сядь в машину, если не видишь родителей. Отойди в людное место, позвони им с чужого телефона и позови на помощь."},
		{"home", "Безопасность дома", "Подозрительное сообщение с просьбой сходить", "Тебе пишет незнакомый человек в игре и просит сходить к другому пользователю за «сокровищем». Просят не рассказывать взрослым.", "Что ты сделаешь?", `[]`, "hard", "free", "Не ходить к незнакомому человеку. Покажи сообщение родителям или взрослому, которому доверяешь."},
		{"city", "Ситуация в городе", "Потерянный телефон", "На остановке ты нашёл дорогой телефон. Рядом никого нет, а через минуту подъезжает автобус.", "Как безопаснее поступить?", `[]`, "medium", "free", "Не забирай найденную вещь домой. Передай её взрослому, сотруднику транспорта или в бюро находок."},
		{"countryside", "Ситуация за городом", "Дикий зверь на тропе", "Во время прогулки в лесу ты увидел дикое животное. Оно пока тебя не заметило.", "Что ты сделаешь?", `[]`, "medium", "free", "Не подходи и не пытайся покормить животное. Тихо отойди тем же путём и сообщи взрослому."},
		{"home", "Безопасность дома", "Запах газа", "Дома ты почувствовал запах газа. Взрослых рядом нет, а телефон находится в другой комнате.", "Что нужно сделать?", `[]`, "hard", "free", "Не включай свет и электроприборы. Выйди из дома, предупреди соседей и позвони в экстренную службу с безопасного места."},
		{"city", "Ситуация в городе", "Разделившийся маршрут", "В торговом центре ты потерял из виду взрослого. Незнакомый человек предлагает вывести тебя на улицу и найти родителей.", "Как лучше поступить?", `[]`, "hard", "free", "Не уходи с незнакомцем. Обратись к сотруднику торгового центра или охраннику и оставайся в людном месте."},
	} {
		if _, err := db.Exec(`INSERT INTO tasks(category,scene,title,description,prompt,choices_json,difficulty,answer_mode,safety_tip)
			SELECT ?,?,?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM tasks WHERE title=?)`,
			task.category, task.scene, task.title, task.description, task.prompt, task.choices, task.difficulty, task.mode, task.tip, task.title); err != nil {
			return err
		}
	}
	return nil
}
