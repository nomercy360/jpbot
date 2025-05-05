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
	"math/rand"
	"strings"
)

type Storager interface {
	GetUser(telegramID int64) (*db.User, error)
	SaveTasksBatch(tasks []db.Exercise) error
	GetExercisesByLevel(level string) ([]db.Exercise, error)
	GetNextExerciseForUser(userID int64, level string, exTypes []string) (db.Exercise, error)
	MarkExerciseSent(userID, exerciseID int64) error
	SaveUser(user *db.User) error
	UpdateUser(user *db.User) error
	GetExerciseByID(exerciseID int64) (db.Exercise, error)
	SaveSubmission(submission db.Submission) error
	ClearUserExercise(userID int64) error
	UpdateUserLevel(userID int64, level string) error
	CountUsers() (int, error)
	GetNextWordForUser(userID int64, level string) (db.Word, error)
	GetWordByID(wordID int64) (db.Word, error)
	SaveWordReview(submission db.TranslationSubmission) error
	MarkWordSent(userID, wordID int64) error
	ClearUserWord(userID int64) error
	UpdateUserRanking(userID int64, score int) error
	GetLeaderboard(periodType db.PeriodType, limit int) ([]db.LeaderboardEntry, error)
	GetUsersPaginated(limit, offset int) ([]db.User, error)
}

type OpenAIClient interface {
	CheckExercise(s db.Submission) (ai.ExerciseFeedback, error)
	GenerateAudio(text string) (io.ReadCloser, error)
	CheckWordTranslation(word, translation, userInput string) (ai.WordTranslationEvaluation, error)
	ExplainSentence(sentence string) (string, error)
}

type handler struct {
	bot          *telegram.Bot
	db           Storager
	openaiClient OpenAIClient
	jwtSecret    string
	botToken     string
}

func NewHandler(
	bot *telegram.Bot,
	db Storager,
	openaiClient *ai.Client,
	jwtSecret string,
	botToken string,
) *handler {
	return &handler{
		bot:          bot,
		db:           db,
		openaiClient: openaiClient,
		jwtSecret:    jwtSecret,
		botToken:     botToken,
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
	var firstName, lastName *string
	var username *string
	if update.Message != nil {
		chatID = update.Message.From.ID
		firstName = &update.Message.From.FirstName
		lastName = &update.Message.From.LastName
		username = &update.Message.From.UserName
	} else if update.CallbackQuery != nil {
		chatID = update.CallbackQuery.From.ID
		firstName = &update.CallbackQuery.From.FirstName
		lastName = &update.CallbackQuery.From.LastName
		username = &update.CallbackQuery.From.UserName
	}

	if username == nil {
		usernameFromID := fmt.Sprintf("user_%d", chatID)
		username = &usernameFromID
	}

	user, err := h.db.GetUser(chatID)

	msg = &telegram.SendMessageParams{
		ChatID: chatID,
	}

	if err != nil && errors.Is(err, db.ErrNotFound) {
		imgUrl := fmt.Sprintf("%s/avatars/%d.svg", "https://assets.peatch.io", rand.Intn(30)+1)

		newUser := &db.User{
			TelegramID: chatID,
			Username:   username,
			FirstName:  firstName,
			LastName:   lastName,
			AvatarURL:  &imgUrl,
			Level:      db.LevelN5,
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
	} else if err != nil {
		log.Printf("Failed to get user: %v", err)
		msg.Text = "Ошибка при получении пользователя. Попробуй позже."
	} else if user.AvatarURL == nil {

		imgUrl := fmt.Sprintf("%s/avatars/%d.svg", "https://assets.peatch.io", rand.Intn(30)+1)

		newUser := &db.User{
			TelegramID: chatID,
			Username:   username,
			FirstName:  firstName,
			LastName:   lastName,
			AvatarURL:  &imgUrl,
		}

		if err := h.db.UpdateUser(newUser); err != nil {
			log.Printf("Failed to update user: %v", err)
		}
	}

	// check if a message is not a command but a callback
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
		count, err := h.db.CountUsers()
		if err != nil {
			log.Printf("Failed to get users: %v", err)
			msg.Text = "Ошибка при получении пользователей."
		} else {
			msg.Text = fmt.Sprintf("Всего пользователей: %d", count)
		}
	case "start":
		msg.Text = "Привет\\! Этот бот для изучения японского языка\\. Он поможет тебе практиковать перевод предложений, слов и грамматику\\!\n\n" +
			"*Как использовать:*\n" +
			"\\- /task — получить задание \\(перевод, вопрос, грамматика или аудио\\)\\.\n" +
			"\\- /vocab — учить новые слова\\.\n" +
			"\\- /level — выбрать уровень сложности \\(N5, N4, N3\\)\\.\n" +
			"🤖 Ответ проверит AI, который даст обратную связь и советы\\. Начинай с /task или /vocab\\!\n\n" +
			"Подписывайся на канал @jpbot\\_learn\\_japanese\\. Там будет информация об обновлениях и обсуждение фич\\."
		msg.ParseMode = models.ParseModeMarkdown
	case "task":
		if user.CurrentExerciseID != nil {
			msg.Text = "У тебя уже есть задание. Попробуй решить его!"
			break
		}

		types := []string{db.ExerciseTypeQuestion, db.ExerciseTypeTranslation, db.ExerciseTypeGrammar, db.ExerciseTypeAudio}

		exercise, err := h.db.GetNextExerciseForUser(chatID, user.Level, types)
		if err != nil && errors.Is(err, db.ErrNotFound) {
			msg.Text = "Задания для твоего уровня закончились. Попробуй зайти завтра!"
		} else if err != nil {
			msg.Text = "Ошибка при получении задания. Попробуй позже."
			log.Printf("Failed to get next exercise: %v", err)
		} else {
			switch exercise.Type {
			case db.ExerciseTypeQuestion:
				c, _ := db.ContentAs[db.QuestionContent](exercise.Content)
				msg.Text = fmt.Sprintf("Задание:\n\nОтветь на вопрос: %s\n\nИспользуй /explain для подсказки", c.Question)
			case db.ExerciseTypeTranslation:
				c, _ := db.ContentAs[db.SentenceContent](exercise.Content)
				msg.Text = fmt.Sprintf("Задание:\n\nПереведи: %s", c.Russian)
			case db.ExerciseTypeGrammar:
				c, _ := db.ContentAs[db.GrammarContent](exercise.Content)
				msg.Text = fmt.Sprintf("🔹*Грамматика:* %s\n💡*Значение:* %s\n🧱*Структура:* %s\n*🗣Пример:* %s\n\nТвой пример:",
					telegram.EscapeMarkdown(c.Grammar),
					telegram.EscapeMarkdown(c.Meaning),
					telegram.EscapeMarkdown(c.Structure),
					telegram.EscapeMarkdown(c.Example),
				)
				msg.ParseMode = models.ParseModeMarkdown
			case db.ExerciseTypeAudio:
				c, _ := db.ContentAs[db.AudioContent](exercise.Content)
				msg.Text = fmt.Sprintf("Задание:\n\nПрослушай аудио и ответь на вопрос: %s\n\nИспользуй /explain для подсказки", c.Question)

				audioReader, err := h.openaiClient.GenerateAudio(c.Text)
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
						Filename: "voice.ogg", // Telegram требует имя
						Data:     bytes.NewReader(audioData),
					},
				}

				if _, err := h.bot.SendVoice(context.Background(), params); err != nil {
					log.Printf("Failed to send audio: %v", err)
					msg.Text = "Ошибка при отправке аудио. Попробуй позже."
					return
				}
			}
			if err := h.db.MarkExerciseSent(user.ID, exercise.ID); err != nil {
				log.Printf("Failed to mark exercise as sent: %v", err)
			}
		}
	case "vocab":
		if user.CurrentWordID != nil {
			msg.Text = "У тебя уже есть задание. Попробуй решить его!"
			break
		}
		word, err := h.db.GetNextWordForUser(user.ID, user.Level)
		if err != nil && errors.Is(err, db.ErrNotFound) {
			msg.Text = "Слова для твоего уровня закончились. Попробуй зайти завтра!"
		} else if err != nil {
			msg.Text = "Ошибка при получении слова. Попробуй позже."
			log.Printf("Failed to get next word: %v", err)
		} else {
			msg.Text = fmt.Sprintf("Переведи слово: *%s*", telegram.EscapeMarkdown(word.Translation))
			msg.ParseMode = models.ParseModeMarkdown
			if err := h.db.MarkWordSent(chatID, word.ID); err != nil {
				log.Printf("Failed to mark word as sent: %v", err)
			}
		}
	case "explain":
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

		var sentence string
		if exercise.Type == db.ExerciseTypeQuestion {
			c, _ := db.ContentAs[db.QuestionContent](exercise.Content)
			sentence = c.Question
		} else if exercise.Type == db.ExerciseTypeAudio {
			c, _ := db.ContentAs[db.AudioContent](exercise.Content)
			sentence = c.Text
		} else {
			msg.Text = "Подсказка доступна только для вопросов и аудио."
			break
		}

		go func() {
			if _, err := h.bot.SendChatAction(context.Background(), &telegram.SendChatActionParams{
				ChatID: chatID,
				Action: models.ChatActionTyping,
			}); err != nil {
				log.Printf("Failed to send typing action: %v", err)
			}
		}()

		explanation, err := h.openaiClient.ExplainSentence(sentence)
		if err != nil {
			msg.Text = "Ошибка при получении подсказки."
			log.Printf("Failed to explain sentence: %v", err)
			break
		}
		msg.Text = explanation
	case "answer":
		if user.CurrentWordID == nil {
			msg.Text = "Сначала получи слово с помощью /vocab."
			break
		}

		word, err := h.db.GetWordByID(*user.CurrentWordID)
		if err != nil {
			msg.Text = "Ошибка при получении слова."
			log.Printf("Failed to get word: %v", err)
			break
		}

		var exampleText string
		if len(word.Examples) > 0 {
			var parts []string
			for _, s := range word.Examples[0].Sentence {
				if s.Furigana != nil {
					parts = append(parts, fmt.Sprintf("%s(%s)", s.Fragment, *s.Furigana))
				} else {
					parts = append(parts, s.Fragment)
				}
			}
			jap := strings.Join(parts, "")
			exampleText = fmt.Sprintf("\n\nПример: %s\n%s\n\nПопробуй снова:", jap, word.Examples[0].Translation)
		}

		msg.Text = fmt.Sprintf("%s%s", word.GetKanji(), exampleText)
		submission := db.TranslationSubmission{
			UserID:    user.ID,
			WordID:    word.ID,
			IsCorrect: false,
		}

		if err := h.db.SaveWordReview(submission); err != nil {
			log.Printf("Failed to save word review: %v", err)
		}
	case "reset":
		if err := h.db.ClearUserExercise(chatID); err != nil {
			log.Printf("Failed to clear user exercise: %v", err)
		}
		msg.Text = "Текущая задача сброшена. Используй /task или /vocab для получения нового задания."
	case "level":
		msg.Text = "Выбери уровень:"
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("N5", "level:N5"),
				tgbotapi.NewInlineKeyboardButtonData("N4", "level:N4"),
				tgbotapi.NewInlineKeyboardButtonData("N3", "level:N3"),
				//tgbotapi.NewInlineKeyboardButtonData("N2", "level:N2"),
				//tgbotapi.NewInlineKeyboardButtonData("N1", "level:N1"),
			),
		)

		msg.ReplyMarkup = &keyboard
	default:
		if user.CurrentExerciseID != nil && user.CurrentMode == db.ModeExercise {
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

			feedbackText := fmt.Sprintf("%s", feedback.Comment)
			if feedback.Suggestion != "" {
				feedbackText += fmt.Sprintf("\n\n%s", feedback.Suggestion)
			}

			submission.GPTFeedback = feedbackText
			submission.IsCorrect = feedback.Score >= 80

			if submission.IsCorrect {
				msg.Text = fmt.Sprintf("Правильно\\! 🎉\n\nЧтобы получить новое задание, используй /task\\.")
				msg.ParseMode = models.ParseModeMarkdown
				user.CurrentExerciseID = nil
				if err := h.db.ClearUserExercise(chatID); err != nil {
					log.Printf("Failed to save user: %v", err)
				}
				// Update rankings
				if err := h.db.UpdateUserRanking(user.TelegramID, 1); err != nil {
					log.Printf("Failed to update user ranking: %v", err)
				}
			} else {
				msg.Text = fmt.Sprintf("Неправильно\\.\n\n%s\n%s\n\nПопробуй еще раз:",
					telegram.EscapeMarkdown(feedback.Comment),
					telegram.EscapeMarkdown(feedback.Suggestion))
				msg.ParseMode = models.ParseModeMarkdown
			}

			if err := h.db.SaveSubmission(submission); err != nil {
				log.Printf("Failed to save submission: %v", err)
				msg.Text = "Ошибка при сохранении ответа."
			}
		} else if user.CurrentWordID != nil && user.CurrentMode == db.ModeVocab {
			userInput := update.Message.Text
			word, err := h.db.GetWordByID(*user.CurrentWordID)
			if err != nil {
				msg.Text = "Ошибка при получении слова."
				log.Printf("Failed to get word: %v", err)
				break
			}

			res, err := h.openaiClient.CheckWordTranslation(word.GetKanji(), word.Translation, userInput)
			if err != nil {
				msg.Text = "Ошибка при проверке ответа."
				log.Printf("Failed to check word translation: %v", err)
				break
			}

			isCorrect := res.Score >= 80

			submission := db.TranslationSubmission{
				UserID:      user.ID,
				WordID:      word.ID,
				Translation: word.Translation,
				IsCorrect:   isCorrect,
			}

			if err := h.db.SaveWordReview(submission); err != nil {
				log.Printf("Failed to save word review: %v", err)
			}

			if isCorrect {
				nextWord, err := h.db.GetNextWordForUser(user.ID, user.Level)
				if err != nil && errors.Is(err, db.ErrNotFound) {
					log.Printf("No more words for user: %v", err)
					msg.Text = "Слова для твоего уровня закончились\\. Попробуй зайти завтра\\!"
				} else if err != nil {
					log.Printf("Failed to get next word: %v", err)
				}

				msg.Text = fmt.Sprintf("Правильно\\! 🎉\n\nСледующее слово: *%s*\n\nЕсли не знаешь, используй /answer", telegram.EscapeMarkdown(nextWord.Translation))

				msg.ParseMode = models.ParseModeMarkdown
				if err := h.db.MarkWordSent(chatID, nextWord.ID); err != nil {
					log.Printf("Failed to mark word as sent: %v", err)
				}
				// Update rankings
				if err := h.db.UpdateUserRanking(user.TelegramID, 1); err != nil {
					log.Printf("Failed to update user ranking: %v", err)
				}
			} else {
				msg.Text = fmt.Sprintf("%s\n\nПопробуй еще раз:", res.Comment)
			}
		} else {
			msg.Text = "Чтобы получить задание, используй /task или /vocab.\n\n" +
				"Если хочешь сменить уровень, используй /level.\n\n"
		}

	}

	return msg
}
