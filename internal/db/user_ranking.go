package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type PeriodType string

const (
	PeriodTypeDaily   PeriodType = "daily"
	PeriodTypeWeekly  PeriodType = "weekly"
	PeriodTypeMonthly PeriodType = "monthly"
)

func (pt PeriodType) IsValid() bool {
	switch pt {
	case PeriodTypeDaily, PeriodTypeWeekly, PeriodTypeMonthly:
		return true
	default:
		return false
	}
}

type UserRanking struct {
	ID          int64      `db:"id"`
	UserID      int64      `db:"user_id"`
	Score       int        `db:"score"`
	PeriodStart time.Time  `db:"period_start"`
	PeriodEnd   time.Time  `db:"period_end"`
	PeriodType  PeriodType `db:"period_type"`
	CreatedAt   time.Time  `db:"created_at"`
}

type LeaderboardEntry struct {
	UserID    int64  `db:"user_id" json:"user_id"`
	Username  string `db:"username" json:"username"`
	FirstName string `db:"first_name" json:"first_name"`
	LastName  string `db:"last_name" json:"last_name"`
	AvatarURL string `db:"avatar_url" json:"avatar_url"`
	Level     string `db:"level" json:"level"`
	Score     int    `db:"score" json:"score"`
	Rank      int    `db:"rank" json:"rank"`
}

func getPeriodRange(now time.Time, periodType PeriodType) (time.Time, time.Time, error) {
	switch periodType {
	case PeriodTypeDaily:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
		return start, end, nil
	case PeriodTypeWeekly:
		start := time.Date(now.Year(), now.Month(), now.Day()-int(now.Weekday())+1, 0, 0, 0, 0, time.UTC)
		end := time.Date(now.Year(), now.Month(), now.Day()-int(now.Weekday())+7, 23, 59, 59, 0, time.UTC)
		return start, end, nil
	case PeriodTypeMonthly:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(now.Year(), now.Month()+1, 0, 23, 59, 59, 0, time.UTC)
		return start, end, nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("invalid period type: %s", periodType)
	}
}

func (s *storage) UpdateUserRanking(userID int64, score int) error {
	now := time.Now()
	periodTypes := []PeriodType{PeriodTypeDaily, PeriodTypeWeekly, PeriodTypeMonthly}
	for _, typ := range periodTypes {
		start, end, err := getPeriodRange(now, typ)
		if err != nil {
			return err
		}
		var existingScore int
		err = s.db.QueryRow(`
			SELECT score FROM user_rankings
			WHERE user_id = ? AND period_start = ? AND period_end = ? AND period_type = ?`,
			userID, start, end, typ,
		).Scan(&existingScore)

		if errors.Is(err, sql.ErrNoRows) {
			// Create new ranking
			_, err = s.db.Exec(`
				INSERT INTO user_rankings (user_id, score, period_start, period_end, period_type)
				VALUES (?, ?, ?, ?, ?)`,
				userID, score, start, end, typ,
			)
		} else if err == nil {
			// Update existing ranking
			_, err = s.db.Exec(`
				UPDATE user_rankings
				SET score = score + ?
				WHERE user_id = ? AND period_start = ? AND period_end = ? AND period_type = ?`,
				score, userID, start, end, typ,
			)
		}

		if err != nil {
			return fmt.Errorf("error updating %s ranking: %w", typ, err)
		}
	}

	return nil
}

func (s *storage) GetLeaderboard(periodType PeriodType, limit int) ([]LeaderboardEntry, error) {
	now := time.Now()
	start, end, err := getPeriodRange(now, periodType)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT 
			u.id AS user_id,
			u.username,
			u.first_name,
			u.last_name,
			u.avatar_url,
			u.level,
			ur.score,
			RANK() OVER (ORDER BY ur.score DESC) AS rank
		FROM user_rankings ur
		JOIN users u ON ur.user_id = u.telegram_id
		WHERE ur.period_type = ?
		AND ur.period_start >= ?
		AND ur.period_end <= ?
		AND u.username IS NOT NULL
		ORDER BY ur.score DESC
		LIMIT ?
	`

	rows, err := s.db.Query(query, string(periodType), start, end, limit)
	if err != nil {
		return nil, fmt.Errorf("error getting leaderboard: %w", err)
	}
	defer rows.Close()

	entries := make([]LeaderboardEntry, 0, limit)
	for rows.Next() {
		var entry LeaderboardEntry
		if err := rows.Scan(
			&entry.UserID,
			&entry.Username,
			&entry.FirstName,
			&entry.LastName,
			&entry.AvatarURL,
			&entry.Level,
			&entry.Score,
			&entry.Rank,
		); err != nil {
			return nil, fmt.Errorf("error scanning leaderboard entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating leaderboard rows: %w", err)
	}

	return entries, nil
}
