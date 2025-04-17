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

type User struct {
	ID                int64  `db:"id"`
	TelegramID        int64  `db:"telegram_id"`
	Level             string `db:"level"`               // уровень (например, 'beginner', 'N5', 'N4')
	Points            int    `db:"points"`              // количество очков
	ExercisesDone     int    `db:"exercises_done"`      // количество выполненных упражнений
	CurrentExerciseID *int64 `db:"current_exercise_id"` // ID текущего упражнения
}

type Exercise struct {
	ID            int64     `db:"id" json:"id"`
	Level         string    `db:"level" json:"level"`
	Question      string    `db:"question" json:"question"`
	CorrectAnswer string    `db:"correct_answer" json:"correct_answer"`
	Topic         string    `db:"topic" json:"topic"`
	Type          string    `db:"type" json:"type"`
	Explanation   string    `db:"explanation" json:"explanation"`
	AudioURL      string    `db:"audio_url" json:"audio_url"`
	AudioText     string    `db:"audio_text" json:"audio_text"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
}

type Submission struct {
	ID          int64     `db:"id"`
	UserID      int64     `db:"user_id"`
	ExerciseID  int64     `db:"exercise_id"`
	UserInput   string    `db:"user_input"`
	GPTFeedback string    `db:"gpt_feedback"`
	IsCorrect   bool      `db:"is_correct"`
	CreatedAt   time.Time `db:"created_at"`
	Exercise    Exercise  `db:"exercise"`
}

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
	LevelBeginner = "BEGINNER"
	LevelN5       = "N5"
	LevelN4       = "N4"
	LevelN3       = "N3"
	LevelN2       = "N2"
	LevelN1       = "N1"
)

func IsValidLevel(level string) bool {
	switch level {
	case LevelBeginner, LevelN5, LevelN4, LevelN3, LevelN2, LevelN1:
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
	       level TEXT DEFAULT 'beginner',
	       points INTEGER DEFAULT 0,
	       exercises_done INTEGER DEFAULT 0,
	       current_exercise_id INTEGER
	   );
	   CREATE TABLE IF NOT EXISTS exercises (
			id INTEGER PRIMARY KEY,
			level TEXT,
			type TEXT DEFAULT 'translation',
			question TEXT,
			correct_answer TEXT,
			topic TEXT,
			explanation TEXT,
			audio_url TEXT,
			audio_text TEXT,
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
		CREATE TABLE IF NOT EXISTS openai_batches (
			id TEXT PRIMARY KEY,
			status TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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

func (s *storage) GetSubmissionByID(ctx context.Context, id int64) (Submission, error) {
	var submission Submission
	query := `SELECT us.id, us.user_id, us.exercise_id, us.user_input, us.gpt_feedback, us.is_correct, us.created_at,
				json_object(
					'id', e.id,
					'level', e.level,
					'question', e.question,
					'correct_answer', e.correct_answer,
					'topic', e.topic,
				    'type', e.type,
					'explanation', e.explanation,
					'audio_url', e.audio_url,
					'audio_text', e.audio_text
				) AS exercise
				FROM user_submissions us
				JOIN exercises e ON us.exercise_id = e.id
				WHERE us.id = ?`

	var exerciseJSON interface{}

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&submission.ID,
		&submission.UserID,
		&submission.ExerciseID,
		&submission.UserInput,
		&submission.GPTFeedback,
		&submission.IsCorrect,
		&submission.CreatedAt,
		&exerciseJSON,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Submission{}, ErrNotFound
		}
		return Submission{}, fmt.Errorf("error getting submission: %w", err)
	}

	exercise, err := UnmarshalJSONToStruct[Exercise](exerciseJSON)
	if err != nil {
		return Submission{}, fmt.Errorf("error unmarshalling exercise JSON: %w", err)
	}

	submission.Exercise = exercise

	return submission, nil
}

func (s *storage) SaveTasksBatch(tasks []Exercise) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
		INSERT INTO exercises (level, question, correct_answer, topic, explanation, audio_url, audio_text, type)
		VALUES (?, ?, ?, '', ?, ?, ?, ?)
	`)

	if err != nil {
		return err
	}

	defer stmt.Close()

	for _, task := range tasks {
		if _, err := stmt.Exec(
			task.Level,
			task.Question,
			task.CorrectAnswer,
			task.Explanation,
			task.AudioURL,
			task.AudioText,
			task.Type,
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *storage) GetUser(telegramID int64) (*User, error) {
	var user User
	query := `SELECT id, telegram_id, level, points, exercises_done, current_exercise_id FROM users WHERE telegram_id = ?`
	err := s.db.QueryRow(query, telegramID).Scan(&user.ID, &user.TelegramID, &user.Level, &user.Points, &user.ExercisesDone, &user.CurrentExerciseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("error getting user: %w", err)
	}
	return &user, nil
}

//GetExercisesByLevel

func (s *storage) GetExercisesByLevel(level string) ([]Exercise, error) {
	query := `SELECT id, level, question, correct_answer, topic, explanation, type, audio_url, audio_text, created_at FROM exercises WHERE level = ?`
	rows, err := s.db.Query(query, level)
	if err != nil {
		return nil, fmt.Errorf("error querying exercises: %w", err)
	}
	defer rows.Close()

	var exercises []Exercise
	for rows.Next() {
		var exercise Exercise
		if err := rows.Scan(
			&exercise.ID,
			&exercise.Level,
			&exercise.Question,
			&exercise.CorrectAnswer,
			&exercise.Topic,
			&exercise.Explanation,
			&exercise.Type,
			&exercise.CreatedAt,
			&exercise.AudioURL,
		); err != nil {
			return nil, fmt.Errorf("error scanning exercise: %w", err)
		}
		exercises = append(exercises, exercise)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return exercises, nil
}

func (s *storage) GetExercisesByLevelAndType(level, exType string) ([]Exercise, error) {
	query := `SELECT id, level, question, correct_answer, topic, explanation, type, audio_url, audio_text, created_at FROM exercises WHERE level = ? AND type = ?`
	rows, err := s.db.Query(query, level, exType)
	if err != nil {
		return nil, fmt.Errorf("error querying exercises: %w", err)
	}
	defer rows.Close()

	var exercises []Exercise
	for rows.Next() {
		var exercise Exercise
		if err := rows.Scan(
			&exercise.ID,
			&exercise.Level,
			&exercise.Question,
			&exercise.CorrectAnswer,
			&exercise.Topic,
			&exercise.Explanation,
			&exercise.Type,
			&exercise.AudioURL,
			&exercise.AudioText,
			&exercise.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("error scanning exercise: %w", err)
		}
		exercises = append(exercises, exercise)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return exercises, nil
}

func (s *storage) GetNextExerciseForUser(userID int64, level string) (Exercise, error) {
	query := `
		SELECT e.id, e.level, e.question, e.correct_answer, e.topic, e.explanation, e.type, e.audio_url, e.audio_text, e.created_at
		FROM exercises e
		LEFT JOIN (
			SELECT exercise_id, COUNT(*) as times_shown
			FROM user_submissions
			WHERE user_id = ?
			GROUP BY exercise_id
		) ue ON e.id = ue.exercise_id
		WHERE e.level = ?
		AND (
			ue.exercise_id IS NULL
			OR (e.type = 'grammar' AND ue.times_shown < 2)
			OR (e.type != 'grammar' AND ue.exercise_id IS NULL)
		)
		ORDER BY RANDOM()
		LIMIT 1
	`

	exercise := Exercise{}

	err := s.db.QueryRow(query, userID, level).Scan(
		&exercise.ID,
		&exercise.Level,
		&exercise.Question,
		&exercise.CorrectAnswer,
		&exercise.Topic,
		&exercise.Explanation,
		&exercise.Type,
		&exercise.AudioURL,
		&exercise.AudioText,
		&exercise.CreatedAt,
	)

	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return exercise, ErrNotFound
	} else if err != nil {
		return exercise, fmt.Errorf("error getting next exercise: %w", err)
	}

	return exercise, nil
}

// MarkExerciseSent отмечает задание как отправленное пользователю
func (s *storage) MarkExerciseSent(userID, exerciseID int64) error {
	updateQuery := `
		UPDATE users	
		SET current_exercise_id = ?	
		WHERE telegram_id = ?
	`

	_, err := s.db.Exec(updateQuery, exerciseID, userID)
	if err != nil {
		return fmt.Errorf("error updating current exercise: %w", err)
	}

	return nil
}

func (s *storage) ClearUserExercise(userID int64) error {
	updateQuery := `
		UPDATE users	
		SET current_exercise_id = NULL	
		WHERE telegram_id = ?
	`

	_, err := s.db.Exec(updateQuery, userID)
	if err != nil {
		return fmt.Errorf("error clearing current exercise: %w", err)
	}

	return nil
}

func (s *storage) UpdateUserLevel(userID int64, level string) error {
	updateQuery := `
		UPDATE users	
		SET level = ?, current_exercise_id = NULL
		WHERE telegram_id = ?
	`

	_, err := s.db.Exec(updateQuery, level, userID)
	if err != nil {
		return fmt.Errorf("error updating user level: %w", err)
	}

	return nil
}

func (s *storage) SaveUser(user *User) error {
	query := `INSERT INTO users (telegram_id, level, points, exercises_done) VALUES (?, ?, ?, ?)`
	_, err := s.db.Exec(query, user.TelegramID, user.Level, user.Points, user.ExercisesDone)
	if err != nil {
		return fmt.Errorf("error saving user: %w", err)
	}
	return nil
}

func (s *storage) GetExerciseByID(exerciseID int64) (Exercise, error) {
	exercise := Exercise{}

	query := `SELECT id, level, question, correct_answer, topic, explanation, type, audio_url, audio_text,  created_at FROM exercises WHERE id = ?`
	err := s.db.QueryRow(query, exerciseID).Scan(
		&exercise.ID,
		&exercise.Level,
		&exercise.Question,
		&exercise.CorrectAnswer,
		&exercise.Topic,
		&exercise.Explanation,
		&exercise.Type,
		&exercise.AudioURL,
		&exercise.AudioText,
		&exercise.CreatedAt,
	)

	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return exercise, ErrNotFound
	} else if err != nil {
		return exercise, fmt.Errorf("error getting exercise: %w", err)
	}

	return exercise, nil
}

//SaveSubmission

func (s *storage) SaveSubmission(submission Submission) error {
	query := `
		INSERT INTO user_submissions (user_id, exercise_id, user_input, gpt_feedback, is_correct)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := s.db.Exec(query,
		submission.UserID,
		submission.ExerciseID,
		submission.UserInput,
		submission.GPTFeedback,
		submission.IsCorrect,
	)

	if err != nil {
		return fmt.Errorf("error saving submission: %w", err)
	}

	return nil
}

func (s *storage) SaveBatchMeta(id string, status string) error {
	_, err := s.db.Exec(`INSERT INTO openai_batches (id, status) VALUES (?, ?)`, id, status)
	return err
}

func (s *storage) GetPendingBatches() ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM openai_batches WHERE status = 'in_progress'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *storage) UpdateBatchStatus(id string, status string) error {
	_, err := s.db.Exec(`UPDATE openai_batches SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *storage) GetAllUsers() ([]User, error) {
	query := `SELECT id, telegram_id, level, points, exercises_done, current_exercise_id FROM users`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("error querying users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.TelegramID, &user.Level, &user.Points, &user.ExercisesDone, &user.CurrentExerciseID); err != nil {
			return nil, fmt.Errorf("error scanning user: %w", err)
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return users, nil
}

func (s *storage) CountUnsolvedExercisesForUser(userID int64, level string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM exercises e
		LEFT JOIN user_submissions us ON e.id = us.exercise_id AND us.user_id = ?
		WHERE e.level = ? AND us.exercise_id IS NULL
	`

	var count int
	err := s.db.QueryRow(query, userID, level).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("error counting unsolved exercises: %w", err)
	}

	return count, nil
}
