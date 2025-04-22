package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Exercise struct {
	ID        int64     `db:"id" json:"id"`
	Level     string    `db:"level" json:"level"`
	Type      string    `db:"type" json:"type"`
	Content   Content   `db:"content" json:"content"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type Content struct {
	Grammar   string `json:"grammar"`
	Meaning   string `json:"meaning"`
	Structure string `json:"structure"`
	Example   string `json:"example"`
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

	var exercises []Exercise
	var contentJSON sql.NullString
	for rows.Next() {
		var exercise Exercise
		if err := rows.Scan(
			&exercise.ID,
			&exercise.Level,
			&contentJSON,
			&exercise.Type,
			&exercise.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("error scanning exercise: %w", err)
		}

		if contentJSON.Valid {
			exercise.Content, err = UnmarshalJSONToStruct[Content](contentJSON.String)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling exercise content: %w", err)
			}
		} else {
			return nil, fmt.Errorf("exercise content is null")
		}

		exercises = append(exercises, exercise)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return exercises, nil
}

func (s *storage) GetExercisesByLevelAndType(level, exType string) ([]Exercise, error) {
	query := `SELECT id, level, content, type, created_at FROM exercises WHERE level = ? AND type = ?`
	rows, err := s.db.Query(query, level, exType)
	if err != nil {
		return nil, fmt.Errorf("error querying exercises: %w", err)
	}
	defer rows.Close()

	var exercises []Exercise
	for rows.Next() {
		var exercise Exercise
		var contentJSON sql.NullString
		if err := rows.Scan(
			&exercise.ID,
			&exercise.Level,
			&contentJSON,
			&exercise.Type,
			&exercise.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("error scanning exercise: %w", err)
		}
		if contentJSON.Valid {
			exercise.Content, err = UnmarshalJSONToStruct[Content](contentJSON.String)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling exercise content: %w", err)
			}
		} else {
			return nil, fmt.Errorf("exercise content is null")
		}
		exercises = append(exercises, exercise)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return exercises, nil
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

	var exercise Exercise
	var contentJSON sql.NullString
	err := s.db.QueryRow(query, args...).Scan(
		&exercise.ID,
		&exercise.Level,
		&contentJSON,
		&exercise.Type,
		&exercise.CreatedAt,
	)

	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return exercise, ErrNotFound
	} else if err != nil {
		return exercise, fmt.Errorf("error getting next exercise: %w", err)
	}

	if contentJSON.Valid {
		exercise.Content, err = UnmarshalJSONToStruct[Content](contentJSON.String)
		if err != nil {
			return exercise, fmt.Errorf("error unmarshalling exercise content: %w", err)
		}
	} else {
		return exercise, fmt.Errorf("exercise content is null")
	}

	return exercise, nil
}

func (s *storage) MarkExerciseSent(userID, exerciseID int64) error {
	updateQuery := `
		UPDATE users	
		SET current_exercise_id = ?	
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
		SET current_exercise_id = NULL	
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
		exercise.Content, err = UnmarshalJSONToStruct[Content](contentJSON.String)
		if err != nil {
			return exercise, fmt.Errorf("error unmarshalling exercise content: %w", err)
		}
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
