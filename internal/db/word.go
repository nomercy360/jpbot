package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Word struct {
	ID          int64     `db:"id"`
	Kanji       *string   `db:"kanji"`
	Kana        string    `db:"kana"`
	Translation string    `db:"translation"`
	Examples    []Example `db:"examples_json"`
	Level       string    `db:"level"`
	AudioURL    string    `db:"audio_url"`
	CreatedAt   time.Time `db:"created_at"`
}

func (w *Word) GetKanji() string {
	if w.Kanji != nil {
		return *w.Kanji
	}

	return w.Kana
}

type Sentence struct {
	Fragment string  `json:"fragment"`
	Furigana *string `json:"furigana"`
}

type Example struct {
	Sentence    []Sentence `json:"sentence"`
	Translation string     `json:"translation"`
}

// Spaced repetition intervals (in hours)
var intervals = []int{4, 8, 24, 48, 168, 336, 720}

func (s *storage) SaveWordsBatch(words []Word) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
		INSERT INTO words (kanji, kana, level, translation, examples_json,  audio_url)
		VALUES (?, ?, ?, ?, ?, ?)
	`)

	if err != nil {
		return err
	}

	defer stmt.Close()

	for _, word := range words {
		examplesJSON, err := json.Marshal(word.Examples)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error marshalling examples to JSON: %w", err)
		}

		if _, err := stmt.Exec(
			word.Kanji,
			word.Kana,
			word.Level,
			word.Translation,
			examplesJSON,
			word.AudioURL,
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *storage) GetNextWordForUser(userID int64, level string) (Word, error) {
	var word Word
	query := `
		SELECT w.id, w.kanji, w.kana, w.translation, w.examples_json, w.level, w.audio_url, w.created_at
			FROM words w
			LEFT JOIN word_reviews wr ON w.id = wr.word_id AND wr.user_id = ?
			WHERE w.level = ? AND (
				(wr.next_review IS NOT NULL AND wr.next_review <= DATETIME('now', 'localtime')) OR
				(wr.word_id IS NULL)
			)
			ORDER BY 
				CASE WHEN wr.next_review IS NOT NULL AND wr.next_review <= DATETIME('now', 'localtime') THEN 0 ELSE 1 END,
				wr.next_review ASC,
				RANDOM()
        LIMIT 1
	`

	var examplesJSON sql.NullString
	err := s.db.QueryRow(query, userID, level).Scan(
		&word.ID,
		&word.Kanji,
		&word.Kana,
		&word.Translation,
		&examplesJSON,
		&word.Level,
		&word.AudioURL,
		&word.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Word{}, ErrNotFound
		}
		return Word{}, fmt.Errorf("error getting next word: %w", err)
	}
	if examplesJSON.Valid {
		examples, err := UnmarshalJSONToStruct[[]Example](examplesJSON.String)
		if err != nil {
			return Word{}, fmt.Errorf("error unmarshalling examples JSON: %w", err)
		}
		word.Examples = examples
	}
	return word, nil
}

// GetWordByID fetches a word by its ID.
func (s *storage) GetWordByID(wordID int64) (Word, error) {
	var word Word
	query := `
       SELECT id, kanji, kana, translation, examples_json, level, audio_url, created_at
       FROM words WHERE id = ?
   `
	var examplesJSON sql.NullString
	err := s.db.QueryRow(query, wordID).Scan(
		&word.ID,
		&word.Kanji,
		&word.Kana,
		&word.Translation,
		&examplesJSON,
		&word.Level,
		&word.AudioURL,
		&word.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Word{}, ErrNotFound
		}
		return Word{}, fmt.Errorf("error getting word by id: %w", err)
	}
	if examplesJSON.Valid {
		examples, err := UnmarshalJSONToStruct[[]Example](examplesJSON.String)
		if err != nil {
			return Word{}, fmt.Errorf("error unmarshalling word examples: %w", err)
		}
		word.Examples = examples
	}
	return word, nil
}

func calculateRepetition(currentRep int, isCorrect bool) int {
	if !isCorrect {
		return 0 // Reset on incorrect answer
	}
	return currentRep + 1
}

func calculateNextReview(currentRep int, isCorrect bool) time.Time {
	if !isCorrect {
		return time.Now().Add(4 * time.Hour) // Quick retry for incorrect
	}

	intervalIndex := currentRep
	if intervalIndex >= len(intervals) {
		intervalIndex = len(intervals) - 1
	}

	return time.Now().Add(time.Duration(intervals[intervalIndex]) * time.Hour)
}

type WordReview struct {
	ID           int       `json:"id"`
	WordID       int       `json:"word_id"`
	UserID       int       `json:"user_id"`
	NextReview   time.Time `json:"next_review"`
	Repetition   int       `json:"repetition"`
	LastReviewed time.Time `json:"last_reviewed"`
}

type TranslationSubmission struct {
	WordID      int64  `json:"word_id"`
	UserID      int64  `json:"user_id"`
	Translation string `json:"translation"`
	IsCorrect   bool   `json:"is_correct"`
}

func (s *storage) SaveWordReview(submission TranslationSubmission) error {
	// Update or create a review record
	var review WordReview
	err := s.db.QueryRow(`
		SELECT id, word_id, user_id, next_review, repetition, last_reviewed
		FROM word_reviews WHERE word_id = ? AND user_id = ?`,
		submission.WordID, submission.UserID,
	).Scan(&review.ID, &review.WordID, &review.UserID, &review.NextReview,
		&review.Repetition, &review.LastReviewed)

	var nextReview time.Time
	var newRepetition int

	if errors.Is(err, sql.ErrNoRows) {
		// New review
		newRepetition = calculateRepetition(0, submission.IsCorrect)
		nextReview = calculateNextReview(0, submission.IsCorrect)
		_, err = s.db.Exec(`
			INSERT INTO word_reviews (word_id, user_id, next_review, repetition, last_reviewed)
			VALUES (?, ?, ?, ?, ?)`,
			submission.WordID, submission.UserID, nextReview, newRepetition, time.Now())
	} else if err != nil {
		return fmt.Errorf("error fetching word review: %w", err)
	} else {
		// Update the existing review
		newRepetition = calculateRepetition(review.Repetition, submission.IsCorrect)
		nextReview = calculateNextReview(review.Repetition, submission.IsCorrect)
		_, err = s.db.Exec(`
			UPDATE word_reviews 
			SET next_review = ?, repetition = ?, last_reviewed = ?
			WHERE word_id = ? AND user_id = ?`,
			nextReview, newRepetition, time.Now(),
			submission.WordID, submission.UserID)
	}

	// Update user stats
	if submission.IsCorrect {
		pointsToAdd := 0.5
		_, err = s.db.Exec(`
		UPDATE users 
		SET points = points + ?, exercises_done = exercises_done + 1
		WHERE id = ?`,
			pointsToAdd,
			submission.UserID,
		)

		if err != nil {
			return fmt.Errorf("error saving word review: %w", err)
		}
	}

	return nil
}

func (s *storage) MarkWordSent(userID, wordID int64) error {
	_, err := s.db.Exec(
		`UPDATE users SET current_word_id = ?, current_mode = 'vocab' WHERE telegram_id = ?`,
		wordID, userID,
	)
	if err != nil {
		return fmt.Errorf("error updating current word: %w", err)
	}
	return nil
}

func (s *storage) ClearUserWord(userID int64) error {
	_, err := s.db.Exec(
		`UPDATE users SET current_word_id = NULL WHERE telegram_id = ?`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("error clearing current word: %w", err)
	}
	return nil
}

func (s *storage) IsWordsInitialized() (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM words`
	err := s.db.QueryRow(query).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("error checking if words are initialized: %w", err)
	}
	return count > 0, nil
}
