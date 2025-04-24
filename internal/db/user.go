package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type User struct {
	ID                int64     `db:"id"`
	TelegramID        int64     `db:"telegram_id"`
	Level             string    `db:"level"`
	Points            float64   `db:"points"`
	ExercisesDone     int       `db:"exercises_done"`
	CurrentExerciseID *int64    `db:"current_exercise_id"`
	CurrentWordID     *int64    `db:"current_word_id"`
	CurrentMode       string    `db:"current_mode"`
	CreatedAt         time.Time `db:"created_at"`
	UpdatedAt         time.Time `db:"updated_at"`
}

const (
	ModeExercise = "exercise"
	ModeVocab    = "vocab"
)

func (s *storage) GetUser(telegramID int64) (*User, error) {
	var user User
	query := `SELECT id, telegram_id, level, points, exercises_done, current_exercise_id, current_word_id, current_mode FROM users WHERE telegram_id = ?`
	err := s.db.QueryRow(query, telegramID).Scan(
		&user.ID,
		&user.TelegramID,
		&user.Level,
		&user.Points,
		&user.ExercisesDone,
		&user.CurrentExerciseID,
		&user.CurrentWordID,
		&user.CurrentMode,
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
	query := `INSERT INTO users (telegram_id, level, points, exercises_done) VALUES (?, ?, ?, ?)`
	_, err := s.db.Exec(query, user.TelegramID, user.Level, user.Points, user.ExercisesDone)
	if err != nil {
		return fmt.Errorf("error saving user: %w", err)
	}
	return nil
}
