package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/labstack/echo/v4"
	"io"
	"jpbot/internal/ai"
	"jpbot/internal/db"
	"log"
	"strings"
)

type Storager interface {
	GetUser(telegramID int64) (*db.User, error)
	SaveTasksBatch(tasks []db.Exercise) error
	GetExercisesByLevel(level string) ([]db.Exercise, error)
	GetNextExerciseForUser(userID int64, level string, exTypes []string) (db.Exercise, error)
	MarkExerciseSent(userID, exerciseID int64) error
	SaveUser(user *db.User) error
	GetExerciseByID(exerciseID int64) (db.Exercise, error)
	SaveSubmission(submission db.Submission) error
	ClearUserExercise(userID int64) error
	UpdateUserLevel(userID int64, level string) error
	SaveBatchMeta(id string, status string) error
	GetPendingBatches() ([]string, error)
	UpdateBatchStatus(id string, status string) error
	GetAllUsers() ([]db.User, error)
}

type OpenAIClient interface {
	CheckExercise(s db.Submission) (ai.ExerciseFeedback, error)
	GenerateAudio(text string) (io.ReadCloser, error)
}

type handler struct {
	bot          *telegram.Bot
	db           Storager
	openaiClient OpenAIClient
}

func NewHandler(bot *telegram.Bot, db Storager, openaiClient *ai.Client) *handler {
	return &handler{
		bot:          bot,
		db:           db,
		openaiClient: openaiClient,
	}
}

func (h *handler) HandleWebhook(c echo.Context) error {
	var update tgbotapi.Update
	if err := c.Bind(&update); err != nil {
		log.Printf("Failed to bind update: %v", err)
		return c.NoContent(400)
	}

	if update.Message == nil && update.CallbackQuery == nil {
		return c.NoContent(200)
	}

	resp := h.handleUpdate(update)
	if _, err := h.bot.SendMessage(context.Background(), resp); err != nil {
		log.Printf("Failed to send message: %v", err)
	}

	return c.NoContent(200)
}

func (h *handler) handleUpdate(update tgbotapi.Update) (msg *telegram.SendMessageParams) {
	var chatID int64
	if update.Message != nil {
		chatID = update.Message.From.ID
	} else if update.CallbackQuery != nil {
		chatID = update.CallbackQuery.From.ID
	}

	user, err := h.db.GetUser(chatID)

	log.Printf("User: %v", user)

	msg = &telegram.SendMessageParams{
		ChatID: chatID,
	}

	if err != nil && errors.Is(err, db.ErrNotFound) {
		newUser := &db.User{
			TelegramID: chatID,
			Level:      db.LevelN3,
		}

		if err := h.db.SaveUser(newUser); err != nil {
			log.Printf("Failed to save user: %v", err)
			msg.Text = "Ошибка при регистрации пользователя. Попробуй позже."
		} else {
			msg.Text = "Добро пожаловать! Используй /start для получения задания."
		}

		user, err = h.db.GetUser(chatID)
		if err != nil {
			log.Printf("Failed to get user after saving: %v", err)
			msg.Text = "Ошибка при получении пользователя. Попробуй позже."
		}
	}

	// check if message is not a command but a callback
	if update.CallbackQuery != nil {
		if strings.HasPrefix(update.CallbackQuery.Data, "level:") {
			level := strings.TrimPrefix(update.CallbackQuery.Data, "level:")
			if db.IsValidLevel(level) {
				if err := h.db.UpdateUserLevel(chatID, level); err != nil {
					log.Printf("Failed to update user level: %v", err)
					msg.Text = "Ошибка при обновлении уровня. Попробуй позже."
				} else {
					msg.Text = fmt.Sprintf("Уровень обновлен на %s!", level)
				}
			} else {
				msg.Text = "Недопустимый уровень. Попробуй снова."
			}

			ack := telegram.AnswerCallbackQueryParams{
				CallbackQueryID: update.CallbackQuery.ID,
				Text:            msg.Text,
				ShowAlert:       false,
				CacheTime:       0,
			}

			if ok, err := h.bot.AnswerCallbackQuery(
				context.Background(),
				&ack,
			); err != nil {
				log.Printf("Failed to answer callback query: %v", err)
			} else if !ok {
				log.Printf("Failed to answer callback query: %v", err)
			} else {
				log.Printf("Answered callback query: %s", msg.Text)
			}

			return
		}
	}

	switch update.Message.Command() {
	case "users":
		users, err := h.db.GetAllUsers()
		if err != nil {
			log.Printf("Failed to get users: %v", err)
			msg.Text = "Ошибка при получении пользователей."
		} else {
			count := len(users)
			msg.Text = fmt.Sprintf("Всего пользователей: %d", count)
		}
	case "start":
		msg.Text = "Привет! Используй /task для перевода предложений и /vocab для перевода слов."
	case "task", "vocab":
		if user.CurrentExerciseID != nil {
			msg.Text = "У тебя уже есть задание. Попробуй решить его!"
			break
		}

		types := []string{db.ExerciseTypeQuestion, db.ExerciseTypeTranslation, db.ExerciseTypeGrammar, db.ExerciseTypeAudio}
		if update.Message.Command() == "vocab" {
			types = []string{db.ExerciseTypeVocab}
		}

		exercise, err := h.db.GetNextExerciseForUser(chatID, user.Level, types)
		if err != nil && errors.Is(err, db.ErrNotFound) {
			msg.Text = "Задания для твоего уровня закончились. Попробуй зайти завтра!"
		} else if err != nil {
			msg.Text = "Ошибка при получении задания. Попробуй позже."
			log.Printf("Failed to get next exercise: %v", err)
		} else {
			switch exercise.Type {
			case db.ExerciseTypeVocab:
				msg.Text = fmt.Sprintf("Переведи слово: %s", exercise.Question)
			case db.ExerciseTypeQuestion:
				msg.Text = fmt.Sprintf("Задание:\n\nОтветь на вопрос: %s\n\nИспользуй /hint для подсказки", exercise.Question)
			case db.ExerciseTypeTranslation:
				msg.Text = fmt.Sprintf("Задание:\n\nПереведи: %s\n\nИспользуй /hint для подсказки", exercise.Question)
			case db.ExerciseTypeGrammar:
				msg.Text = fmt.Sprintf("Задание:\n\n%s\n\nТвой пример:", exercise.Question)
			case db.ExerciseTypeAudio:
				msg.Text = fmt.Sprintf("Задание:\n\nПрослушай аудио и ответь на вопрос: %s", exercise.Question)

				audioReader, err := h.openaiClient.GenerateAudio(exercise.AudioText)
				if err != nil {
					log.Printf("Failed to generate audio: %v", err)
					msg.Text = "Ошибка при генерации аудио. Попробуй позже."
					return
				}
				defer audioReader.Close()

				// Считываем всё содержимое в память (аналогично os.ReadFile в примере)
				audioData, err := io.ReadAll(audioReader)
				if err != nil {
					log.Printf("Failed to read audio data: %v", err)
					msg.Text = "Ошибка при обработке аудио. Попробуй позже."
					return
				}

				params := &telegram.SendVoiceParams{
					ChatID: chatID,
					Voice: &models.InputFileUpload{
						Filename: "voice.ogg",                // Telegram требует имя
						Data:     bytes.NewReader(audioData), // Аналогично примеру с фото
					},
					Caption: "Прослушай и ответь на вопрос",
				}

				if _, err := h.bot.SendVoice(context.Background(), params); err != nil {
					log.Printf("Failed to send audio: %v", err)
					msg.Text = "Ошибка при отправке аудио. Попробуй позже."
					return
				}
			}
			if err := h.db.MarkExerciseSent(chatID, exercise.ID); err != nil {
				log.Printf("Failed to mark exercise as sent: %v", err)
			}
		}
	case "hint":
		if user.CurrentExerciseID == nil {
			msg.Text = "Сначала получи задание с помощью /task."
			break
		}

		exercise, err := h.db.GetExerciseByID(*user.CurrentExerciseID)
		if err != nil {
			msg.Text = "Ошибка при получении задания."
			log.Printf("Failed to get exercise: %v", err)
			break
		}
		msg.Text = exercise.Explanation
	case "level":
		msg.Text = "Выбери уровень:"
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Beginner", "level:BEGINNER"),
				tgbotapi.NewInlineKeyboardButtonData("N5", "level:N5"),
				tgbotapi.NewInlineKeyboardButtonData("N4", "level:N4"),
				tgbotapi.NewInlineKeyboardButtonData("N3", "level:N3"),
				tgbotapi.NewInlineKeyboardButtonData("N2", "level:N2"),
				tgbotapi.NewInlineKeyboardButtonData("N1", "level:N1"),
			),
		)

		msg.ReplyMarkup = &keyboard
	default:
		if user.CurrentExerciseID == nil {
			msg.Text = "Сначала получи задание с помощью /task."
			break
		}

		userInput := update.Message.Text
		exercise, err := h.db.GetExerciseByID(*user.CurrentExerciseID)
		if err != nil {
			msg.Text = "Ошибка при проверке задания."
			log.Printf("Failed to get exercise: %v", err)
			break
		}

		submission := db.Submission{
			UserID:     chatID,
			ExerciseID: exercise.ID,
			UserInput:  userInput,
			Exercise:   exercise,
		}

		feedback, err := h.openaiClient.CheckExercise(submission)
		if err != nil {
			msg.Text = "Ошибка при проверке ответа."
			log.Printf("Failed to check exercise: %v", err)
			break
		}

		submission.GPTFeedback = fmt.Sprintf("Оценка: %d, Комментарий: %s, Предложение: %s",
			feedback.Score, feedback.Feedback, feedback.Suggestion)

		submission.IsCorrect = feedback.Score >= 80

		if submission.IsCorrect && submission.Exercise.Type == db.ExerciseTypeVocab {
			next, err := h.db.GetNextExerciseForUser(chatID, user.Level, []string{db.ExerciseTypeVocab})

			if err != nil {
				msg.Text = "Ошибка при получении нового слова."
				log.Printf("Failed to get next vocab exercise: %v", err)
			} else {
				msg.Text = fmt.Sprintf("Правильно! 🎉\n\nКомментарий: %s\n\nСледующее слово: %s\n\n",
					feedback.Feedback, next.Question)
				if err := h.db.MarkExerciseSent(chatID, next.ID); err != nil {
					log.Printf("Failed to mark vocab exercise as sent: %v", err)
				}
			}
		} else if submission.IsCorrect {
			msg.Text = fmt.Sprintf("Правильно! 🎉\n\nКомментарий: %s\n\nПредложение: %s\n\nЧтобы получить новое задание, используй /task.",
				feedback.Feedback, feedback.Suggestion)
			user.CurrentExerciseID = nil
			if err := h.db.ClearUserExercise(chatID); err != nil {
				log.Printf("Failed to save user: %v", err)
			}
		} else {
			msg.Text = fmt.Sprintf("Неправильно. Попробуй еще раз!\n\n%s", feedback.Feedback)
		}

		if err := h.db.SaveSubmission(submission); err != nil {
			log.Printf("Failed to save submission: %v", err)
			msg.Text = "Ошибка при сохранении ответа."
		}
	}

	return msg
}

func (h *handler) HandleListTasks(c echo.Context) error {
	level := strings.ToUpper(c.QueryParam("level"))
	if level == "" {
		level = db.LevelBeginner
	}

	if db.IsValidLevel(level) == false {
		log.Printf("Invalid level: %s", level)
		return c.JSON(400, map[string]string{"error": "invalid level"})
	}

	exercises, err := h.db.GetExercisesByLevel(level)
	if err != nil {
		log.Printf("Failed to get exercises: %v", err)
		return c.JSON(500, map[string]string{"error": "failed to get exercises"})
	}

	return c.JSON(200, exercises)
}
