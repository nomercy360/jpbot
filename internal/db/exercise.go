package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Exercise struct {
	ID        int64           `db:"id" json:"id"`
	Level     string          `db:"level" json:"level"`
	Type      string          `db:"type" json:"type"`
	Content   json.RawMessage `db:"content"   json:"content"`
	CreatedAt time.Time       `db:"created_at" json:"created_at"`
}

func ContentAs[T any](content json.RawMessage) (T, error) {
	var result T
	if err := json.Unmarshal(content, &result); err != nil {
		return result, fmt.Errorf("error unmarshalling content: %w", err)
	}
	return result, nil
}

type GrammarContent struct {
	Grammar   string `json:"grammar"`
	Meaning   string `json:"meaning"`
	Structure string `json:"structure"`
	Example   string `json:"example"`
}

type TranslationContent struct {
	Example string `json:"example"`
}

type QuestionContent struct {
	Question string `json:"question"`
}

type AudioContent struct {
	Text     string `json:"text"`
	Question string `json:"question"`
}

type SentenceContent struct {
	Japanese string `json:"japanese"`
	Russian  string `json:"russian"`
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

func (s *storage) GetSubmissionByID(ctx context.Context, id int64) (Submission, error) {
	var submission Submission
	query := `SELECT us.id, us.user_id, us.exercise_id, us.user_input, us.gpt_feedback, us.is_correct, us.created_at,
				json_object(
					'id', e.id,
					'level', e.level,
					'content', e.content,
				    'type', e.type
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
		INSERT INTO exercises (level, content, type)
		VALUES (?, ?, ?)
	`)

	if err != nil {
		return err
	}

	defer stmt.Close()

	for _, task := range tasks {
		contentJSON, err := MarshalStructToJSON(task.Content)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error marshalling task content: %w", err)
		}

		if _, err := stmt.Exec(
			task.Level,
			contentJSON,
			task.Type,
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *storage) GetExercisesByLevel(level string) ([]Exercise, error) {
	query := `SELECT id, level, content, type, created_at FROM exercises WHERE level = ?`
	rows, err := s.db.Query(query, level)
	if err != nil {
		return nil, fmt.Errorf("error querying exercises: %w", err)
	}
	defer rows.Close()

	var out []Exercise
	var raw sql.NullString
	for rows.Next() {
		var e Exercise
		if err := rows.Scan(
			&e.ID,
			&e.Level,
			&raw,
			&e.Type,
			&e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("error scanning exercise: %w", err)
		}

		if raw.Valid {
			e.Content = json.RawMessage(raw.String)
		}

		out = append(out, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return out, nil
}

func (s *storage) GetExercisesByLevelAndType(level, exType string) ([]Exercise, error) {
	query := `SELECT id, level, content, type, created_at FROM exercises WHERE level = ? AND type = ?`
	rows, err := s.db.Query(query, level, exType)
	if err != nil {
		return nil, fmt.Errorf("error querying exercises: %w", err)
	}
	defer rows.Close()

	var out []Exercise
	for rows.Next() {
		var e Exercise
		var raw sql.NullString
		if err := rows.Scan(
			&e.ID,
			&e.Level,
			&raw,
			&e.Type,
			&e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("error scanning exercise: %w", err)
		}

		if raw.Valid {
			e.Content = json.RawMessage(raw.String)
		} else {
			return nil, fmt.Errorf("exercise content is null")
		}

		out = append(out, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return out, nil
}
func (s *storage) GetNextExerciseForUser(userID int64, level string, exTypes []string) (Exercise, error) {
	placeholders := make([]string, len(exTypes))
	args := []interface{}{userID, level}
	for i, t := range exTypes {
		placeholders[i] = "?"
		args = append(args, t)
	}

	query := fmt.Sprintf(`
		SELECT e.id, e.level, e.content, e.type, e.created_at
		FROM exercises e
		LEFT JOIN (
			SELECT exercise_id, COUNT(*) as times_shown
			FROM user_submissions
			WHERE user_id = ?
			GROUP BY exercise_id
		) ue ON e.id = ue.exercise_id
		WHERE e.level = ? AND e.type IN (%s)
		AND (
			ue.exercise_id IS NULL
			OR (e.type = 'grammar' AND ue.times_shown < 2)
			OR (e.type != 'grammar' AND ue.exercise_id IS NULL)
		)
		ORDER BY RANDOM()
		LIMIT 1
	`, strings.Join(placeholders, ","))

	var e Exercise
	var raw sql.NullString
	err := s.db.QueryRow(query, args...).Scan(
		&e.ID,
		&e.Level,
		&raw,
		&e.Type,
		&e.CreatedAt,
	)

	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	} else if err != nil {
		return e, fmt.Errorf("error getting next exercise: %w", err)
	}

	if raw.Valid {
		e.Content = json.RawMessage(raw.String)
	} else {
		return e, fmt.Errorf("exercise content is null")
	}

	return e, nil
}

func (s *storage) MarkExerciseSent(userID, exerciseID int64) error {
	updateQuery := `
		UPDATE users	
		SET current_exercise_id = ?, current_mode = 'exercise'
		WHERE id = ?
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
		SET current_exercise_id = NULL, current_word_id = NULL, current_mode = 'exercise'
		WHERE telegram_id = ?
	`

	_, err := s.db.Exec(updateQuery, userID)
	if err != nil {
		return fmt.Errorf("error clearing current exercise: %w", err)
	}

	return nil
}

func (s *storage) GetExerciseByID(exerciseID int64) (Exercise, error) {
	exercise := Exercise{}

	query := `SELECT id, level, content, type, created_at FROM exercises WHERE id = ?`
	var contentJSON sql.NullString
	err := s.db.QueryRow(query, exerciseID).Scan(
		&exercise.ID,
		&exercise.Level,
		&contentJSON,
		&exercise.Type,
		&exercise.CreatedAt,
	)

	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return exercise, ErrNotFound
	} else if err != nil {
		return exercise, fmt.Errorf("error getting exercise: %w", err)
	}

	if contentJSON.Valid {
		exercise.Content = json.RawMessage(contentJSON.String)
	} else {
		return exercise, fmt.Errorf("exercise content is null")
	}

	return exercise, nil
}

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

func (s *storage) IsExercisesInitialized() (bool, error) {
	query := `SELECT COUNT(*) FROM exercises`
	var count int
	err := s.db.QueryRow(query).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("error checking if exercises are initialized: %w", err)
	}
	return count > 0, nil
}
