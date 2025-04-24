package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mattn/go-sqlite3"
	_ "github.com/mattn/go-sqlite3"
	"time"
)

type HealthStats struct {
	Status            string `json:"status"`
	Error             string `json:"error,omitempty"`
	Message           string `json:"message"`
	OpenConnections   int    `json:"open_connections"`
	InUse             int    `json:"in_use"`
	Idle              int    `json:"idle"`
	WaitCount         int64  `json:"wait_count"`
	WaitDuration      string `json:"wait_duration"`
	MaxIdleClosed     int64  `json:"max_idle_closed"`
	MaxLifetimeClosed int64  `json:"max_lifetime_closed"`
}

const (
	LevelN5 = "N5"
	LevelN4 = "N4"
	LevelN3 = "N3"
	LevelN2 = "N2"
	LevelN1 = "N1"
)

func IsValidLevel(level string) bool {
	switch level {
	case LevelN5, LevelN4, LevelN3, LevelN2, LevelN1:
		return true
	default:
		return false
	}
}

func UnmarshalJSONToStruct[T any](src interface{}) (T, error) {
	var source []byte
	var zeroValue T

	switch s := src.(type) {
	case []byte:
		source = s
	case string:
		source = []byte(s)
	case nil:
		return zeroValue, nil
	default:
		return zeroValue, fmt.Errorf("unsupported type: %T", s)
	}

	var result T
	if err := json.Unmarshal(source, &result); err != nil {
		return zeroValue, fmt.Errorf("error unmarshalling JSON: %w", err)
	}

	return result, nil
}

func MarshalStructToJSON[T any](src T) ([]byte, error) {
	data, err := json.Marshal(src)
	if err != nil {
		return nil, fmt.Errorf("error marshalling struct to JSON: %w", err)
	}
	return data, nil
}

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

type storage struct {
	db *sql.DB
}

func init() {
	// Registers the sqlite3 driver with a ConnectHook so that we can
	// initialize the default PRAGMAs.
	//
	// Note 1: we don't define the PRAGMA as part of the dsn string
	// because not all pragmas are available.
	//
	// Note 2: the busy_timeout pragma must be first because
	// the connection needs to be set to block on busy before WAL mode
	// is set in case it hasn't been already set by another connection.
	sql.Register("sql",
		&sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				_, err := conn.Exec(`
					PRAGMA busy_timeout       = 10000;
					PRAGMA journal_mode       = WAL;
					PRAGMA journal_size_limit = 200000000;
					PRAGMA synchronous        = NORMAL;
					PRAGMA foreign_keys       = ON;
					PRAGMA temp_store         = MEMORY;
					PRAGMA cache_size         = -16000;
				`, nil)

				return err
			},
		},
	)
}

func ConnectDB(dbPath string) (*storage, error) {
	db, err := sql.Open("sql", dbPath)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	schema := `
	   CREATE TABLE IF NOT EXISTS users (
		   id INTEGER PRIMARY KEY,
		   telegram_id INTEGER UNIQUE,
		   level TEXT DEFAULT 'N5',
		   points INTEGER DEFAULT 0,
		   exercises_done INTEGER DEFAULT 0,
		   current_exercise_id INTEGER,
		   current_word_id INTEGER,
		   current_mode TEXT DEFAULT 'exercise',
		   created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		   updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		   FOREIGN KEY (current_exercise_id) REFERENCES exercises(id),
		   FOREIGN KEY (current_word_id) REFERENCES words(id) 
	   );
	   CREATE TABLE IF NOT EXISTS exercises (
			id INTEGER PRIMARY KEY,
			level TEXT,
			type TEXT DEFAULT 'translation',
			content TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	   );
		CREATE TABLE IF NOT EXISTS user_submissions (
			id INTEGER PRIMARY KEY,
			user_id INTEGER,
			exercise_id INTEGER,
			user_input TEXT,
			gpt_feedback TEXT,
			is_correct BOOLEAN,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS words (
			id INTEGER PRIMARY KEY,
			kanji TEXT,
			kana TEXT NOT NULL,
			translation TEXT NOT NULL,
			examples_json TEXT,
			level TEXT,
			audio_url TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);		
		CREATE TABLE IF NOT EXISTS word_reviews (
    		id INTEGER PRIMARY KEY,
			word_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			next_review TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			repetition INTEGER DEFAULT 0,
			last_reviewed TIMESTAMP,
			FOREIGN KEY (word_id) REFERENCES words(id),
			FOREIGN KEY (user_id) REFERENCES users(id),
			UNIQUE (word_id, user_id)
		);
		`

	_, err = db.Exec(schema)
	if err != nil {
		return nil, err
	}

	return &storage{db: db}, nil
}

func NewStorage(db *sql.DB) *storage {
	return &storage{
		db: db,
	}
}

var (
	ExerciseTypeTranslation = "translation"
	ExerciseTypeQuestion    = "question"
	ExerciseTypeAudio       = "audio"
	ExerciseTypeGrammar     = "grammar"
)

func (s *storage) Health() (HealthStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	stats := HealthStats{}

	// Ping the database
	err := s.db.PingContext(ctx)
	if err != nil {
		stats.Status = "down"
		stats.Error = fmt.Sprintf("db down: %v", err)
		return stats, fmt.Errorf("db down: %w", err)
	}

	// Database is up, add more statistics
	stats.Status = "up"
	stats.Message = "It's healthy"

	// Get database stats (like open connections, in use, idle, etc.)
	dbStats := s.db.Stats()
	stats.OpenConnections = dbStats.OpenConnections
	stats.InUse = dbStats.InUse
	stats.Idle = dbStats.Idle
	stats.WaitCount = dbStats.WaitCount
	stats.WaitDuration = dbStats.WaitDuration.String()
	stats.MaxIdleClosed = dbStats.MaxIdleClosed
	stats.MaxLifetimeClosed = dbStats.MaxLifetimeClosed

	// Evaluate stats to provide a health message
	if dbStats.OpenConnections > 40 { // Assuming 50 is the max for this example
		stats.Message = "The database is experiencing heavy load."
	}

	if dbStats.WaitCount > 1000 {
		stats.Message = "The database has a high number of wait events, indicating potential bottlenecks."
	}

	if dbStats.MaxIdleClosed > int64(dbStats.OpenConnections)/2 {
		stats.Message = "Many idle connections are being closed, consider revising the connection pool settings."
	}

	if dbStats.MaxLifetimeClosed > int64(dbStats.OpenConnections)/2 {
		stats.Message = "Many connections are being closed due to max lifetime, consider increasing max lifetime or revising the connection usage pattern."
	}

	return stats, nil
}

func (s *storage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
