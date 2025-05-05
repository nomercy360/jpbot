package handlers

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"jpbot/internal/db"
	"strconv"
)

type LeaderBoardResponse struct {
	Daily   []db.LeaderboardEntry `json:"daily"`
	Weekly  []db.LeaderboardEntry `json:"weekly"`
	Monthly []db.LeaderboardEntry `json:"monthly"`
}

func (h *handler) HandleLeaderboard(c echo.Context) error {
	limitStr := c.QueryParam("limit")
	limit, err := parseIntQueryParam(limitStr)
	if err != nil {
		return echo.NewHTTPError(400, "invalid limit query parameter")
	}

	daily, err := h.db.GetLeaderboard(db.PeriodTypeDaily, limit)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("error getting daily leaderboard: %v", err))
	}

	weekly, err := h.db.GetLeaderboard(db.PeriodTypeWeekly, limit)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("error getting weekly leaderboard: %v", err))
	}

	monthly, err := h.db.GetLeaderboard(db.PeriodTypeMonthly, limit)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("error getting monthly leaderboard: %v", err))
	}

	response := LeaderBoardResponse{
		Daily:   daily,
		Weekly:  weekly,
		Monthly: monthly,
	}

	return c.JSON(200, response)
}

func parseIntQueryParam(param string) (int, error) {
	if param == "" {
		return 0, fmt.Errorf("query parameter is required")
	}

	value, err := strconv.Atoi(param)
	if err != nil {
		return 0, fmt.Errorf("invalid query parameter: %w", err)
	}

	return value, nil
}
