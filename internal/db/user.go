package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type User struct {
	ID                int64      `db:"id" json:"id"`
	TelegramID        int64      `db:"telegram_id" json:"telegram_id"`
	Level             string     `db:"level" json:"level"`
	Points            float64    `db:"points" json:"points"`
	ExercisesDone     int        `db:"exercises_done" json:"exercises_done"`
	CurrentExerciseID *int64     `db:"current_exercise_id" json:"current_exercise_id"`
	CurrentWordID     *int64     `db:"current_word_id" json:"current_word_id"`
	CurrentMode       string     `db:"current_mode" json:"current_mode"`
	LastName          *string    `db:"last_name" json:"last_name"`
	FirstName         *string    `db:"first_name" json:"first_name"`
	Username          *string    `db:"username" json:"username"`
	AvatarURL         *string    `db:"avatar_url" json:"avatar_url"`
	CreatedAt         time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at" json:"updated_at"`
	BlockedAt         *time.Time `db:"blocked_at" json:"blocked_at"`
}

const (
	ModeExercise = "exercise"
	ModeVocab    = "vocab"
)

func (s *storage) GetUser(telegramID int64) (*User, error) {
	var user User
	query := `SELECT id, telegram_id, username, avatar_url, first_name, last_name, level, points, exercises_done, current_exercise_id, current_word_id, current_mode, created_at, updated_at FROM users WHERE telegram_id = ?`
	err := s.db.QueryRow(query, telegramID).Scan(
		&user.ID,
		&user.TelegramID,
		&user.Username,
		&user.AvatarURL,
		&user.FirstName,
		&user.LastName,
		&user.Level,
		&user.Points,
		&user.ExercisesDone,
		&user.CurrentExerciseID,
		&user.CurrentWordID,
		&user.CurrentMode,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("error getting user: %w", err)
	}
	return &user, nil
}

func (s *storage) CountUsers() (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM users`
	err := s.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("error counting users: %w", err)
	}
	return count, nil
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
	query := `
		INSERT INTO users 
		    (telegram_id, level, username, avatar_url, first_name, last_name)
		VALUES (?, ?, ?, ?, ?, ?)`

	_, err := s.db.Exec(query, user.TelegramID, user.Level, user.Username, user.AvatarURL, user.FirstName, user.LastName)
	if err != nil {
		return fmt.Errorf("error saving user: %w", err)
	}

	return nil
}

func (s *storage) UpdateUser(user *User) error {
	query := `
		UPDATE users
		SET username = ?, avatar_url = ?, first_name = ?, last_name = ?
		WHERE telegram_id = ?`

	_, err := s.db.Exec(query,
		user.Username, user.AvatarURL, user.FirstName, user.LastName, user.TelegramID)
	if err != nil {
		return fmt.Errorf("error updating user: %w", err)
	}

	return nil
}

// GetUsersPaginated returns users ordered by creation time with pagination.
func (s *storage) GetUsersPaginated(limit, offset int) ([]User, error) {
	query := `SELECT id, telegram_id, username, avatar_url, first_name, last_name, level, points, exercises_done, current_exercise_id, current_word_id, current_mode, created_at, updated_at, blocked_at FROM users ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := s.db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("error getting paginated users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.TelegramID, &u.Username, &u.AvatarURL, &u.FirstName, &u.LastName, &u.Level, &u.Points, &u.ExercisesDone, &u.CurrentExerciseID, &u.CurrentWordID, &u.CurrentMode, &u.CreatedAt, &u.UpdatedAt, &u.BlockedAt); err != nil {
			return nil, fmt.Errorf("error scanning user row: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating user rows: %w", err)
	}
	return users, nil
}

// SetUserBlocked updates the blocked_at timestamp for a user.
func (s *storage) SetUserBlocked(userID int64, blocked bool) error {
	var query string
	if blocked {
		query = `UPDATE users SET blocked_at = CURRENT_TIMESTAMP WHERE id = ?`
	} else {
		query = `UPDATE users SET blocked_at = NULL WHERE id = ?`
	}
	if _, err := s.db.Exec(query, userID); err != nil {
		return fmt.Errorf("error updating user blocked status: %w", err)
	}
	return nil
}
