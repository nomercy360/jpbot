package handlers

import (
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	initdata "github.com/telegram-mini-apps/init-data-golang"
	"jpbot/internal/db"
	"log"
	"math/rand"
	"net/http"
	"time"
)

type JWTClaims struct {
	jwt.RegisteredClaims
   UID     int64 `json:"uid,omitempty"`
   ChatID  int64 `json:"chat_id,omitempty"`
   IsAdmin bool  `json:"is_admin,omitempty"`
}

type AuthTelegramRequest struct {
	Query string `json:"query"`
}

type AuthTelegramResponse struct {
	Token string  `json:"token"`
	User  db.User `json:"user"`
}

func (a AuthTelegramRequest) Validate() error {
	if a.Query == "" {
		return fmt.Errorf("query cannot be empty")
	}
	return nil
}

func (h *handler) TelegramAuth(c echo.Context) error {
	var req AuthTelegramRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to bind request")
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	log.Printf("AuthTelegram: %+v", req)

	expIn := 24 * time.Hour
	botToken := h.botToken

	if err := initdata.Validate(req.Query, botToken, expIn); err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid init data from telegram")
	}

	data, err := initdata.Parse(req.Query)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "cannot parse init data from telegram")
	}

	user, err := h.db.GetUser(data.User.ID)
	if err != nil && errors.Is(err, db.ErrNotFound) {
		username := data.User.Username
		if username == "" {
			username = "user_" + fmt.Sprintf("%d", data.User.ID)
		}

		var first, last *string
		if data.User.FirstName != "" {
			first = &data.User.FirstName
		}
		if data.User.LastName != "" {
			last = &data.User.LastName
		}

		imgUrl := fmt.Sprintf("%s/avatars/%d.svg", "https://assets.peatch.io", rand.Intn(30)+1)
		create := db.User{
			Username:   &username,
			TelegramID: data.User.ID,
			FirstName:  first,
			LastName:   last,
			AvatarURL:  &imgUrl,
		}

		if err = h.db.SaveUser(&create); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to save user").SetInternal(err)
		}

		user, err = h.db.GetUser(data.User.ID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user").SetInternal(err)
		}
	} else if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user").SetInternal(err)
	}

	token, err := generateJWT(user.ID, user.TelegramID, h.jwtSecret)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate JWT").SetInternal(err)
	}

	resp := AuthTelegramResponse{
		Token: token,
		User:  *user,
	}

	return c.JSON(http.StatusOK, resp)
}

func generateJWT(userID int64, chatID int64, secretKey string) (string, error) {
	claims := &JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
		UID:    userID,
		ChatID: chatID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	t, err := token.SignedString([]byte(secretKey))
	if err != nil {
		return "", err
	}

	return t, nil
}
