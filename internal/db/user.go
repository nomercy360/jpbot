package db

import (
	"database/sql"
	"errors"
	"fmt"
)

type User struct {
	ID                int64   `db:"id"`
	TelegramID        int64   `db:"telegram_id"`
	Level             string  `db:"level"`
	Points            float64 `db:"points"`
	ExercisesDone     int     `db:"exercises_done"`
	CurrentExerciseID *int64  `db:"current_exercise_id"`
	CurrentWordID     *int64  `db:"current_word_id"`
}

func (s *storage) GetUser(telegramID int64) (*User, error) {
	var user User
	query := `SELECT id, telegram_id, level, points, exercises_done, current_exercise_id, current_word_id FROM users WHERE telegram_id = ?`
	err := s.db.QueryRow(query, telegramID).Scan(
		&user.ID,
		&user.TelegramID,
		&user.Level,
		&user.Points,
		&user.ExercisesDone,
		&user.CurrentExerciseID,
		&user.CurrentWordID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("error getting user: %w", err)
	}
	return &user, nil
}

func (s *storage) GetAllUsers() ([]User, error) {
	query := `SELECT id, telegram_id, level, points, exercises_done, current_exercise_id, current_word_id FROM users`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("error querying users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(
			&user.ID,
			&user.TelegramID,
			&user.Level,
			&user.Points,
			&user.ExercisesDone,
			&user.CurrentExerciseID,
			&user.CurrentWordID,
		); err != nil {
			return nil, fmt.Errorf("error scanning user: %w", err)
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return users, nil
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
