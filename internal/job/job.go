package job

import (
	"context"
	"fmt"
	telegram "github.com/go-telegram/bot"
	"jpbot/internal/ai"
	"jpbot/internal/db"
	"log"
	"time"
)

type Storager interface {
	SaveTasksBatch(tasks []db.Exercise) error
	GetExercisesByLevel(level string) ([]db.Exercise, error)
	SaveBatchMeta(id string, status string) error
	GetPendingBatches() ([]string, error)
	UpdateBatchStatus(id string, status string) error
	GetExercisesByLevelAndType(level, exType string) ([]db.Exercise, error)
	GetAllUsers() ([]db.User, error)
	CountUnsolvedExercisesForUser(userID int64, level string) (int, error)
}

type OpenAIClient interface {
	// CreateBatchTask(levels, types []string, existing map[string][]string, tasksPerLevel int) (*ai.BatchTask, error)
	CreateBatchTask(level string, types []string, existing map[string][]string, tasksPerLevel int) ([]ai.BatchResult, error)
	GetBatchResults(batchID string) ([]ai.BatchResult, bool, error)
}

type job struct {
	bot          *telegram.Bot
	db           Storager
	openaiClient OpenAIClient
}

func NewJob(db Storager, openaiClient *ai.Client, bot *telegram.Bot) *job {
	return &job{
		bot:          bot,
		db:           db,
		openaiClient: openaiClient,
	}
}

func withRetry[T any](attempts int, delay time.Duration, fn func() (T, error)) (T, error) {
	var lastErr error
	var zero T
	for i := 0; i < attempts; i++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		log.Printf("Attempt %d failed: %v. Retrying in %s...", i+1, err, delay)
		time.Sleep(delay)
	}
	return zero, fmt.Errorf("all %d retry attempts failed: %w", attempts, lastErr)
}

func (j *job) generateTasks() error {
	levels := []string{db.LevelN5, db.LevelN4, db.LevelN3, db.LevelBeginner}
	types := []string{db.ExerciseTypeGrammar, db.ExerciseTypeAudio, db.ExerciseTypeTranslation, db.ExerciseTypeQuestion}
	existing := make(map[string][]string)

	for _, level := range levels {
		for _, exType := range types {
			exercises, err := j.db.GetExercisesByLevelAndType(level, exType)
			if err != nil {
				return fmt.Errorf("failed to get exercises for level %s and type %s: %w", level, exType, err)
			}

			for _, exercise := range exercises {
				key := fmt.Sprintf("%s|%s", exType, level)
				existing[key] = append(existing[key], exercise.Question)
			}
		}
	}

	for _, level := range levels {
		log.Printf("Starting task generation for level: %s, types: %v", level, types)

		batch, err := withRetry(3, 5*time.Second, func() ([]ai.BatchResult, error) {
			return j.openaiClient.CreateBatchTask(level, types, existing, 10)
		})

		if err != nil {
			log.Printf("Failed to generate tasks for level %s: %v", level, err)
			continue
		}

		var exercises []db.Exercise
		for _, res := range batch {
			switch res.Type {
			case db.ExerciseTypeQuestion:
				taskList, ok := res.GeneratedTaskList.(ai.QuestionTaskList)
				if !ok {
					log.Printf("Failed to convert task list to QuestionTaskList: %v", res.GeneratedTaskList)
					continue
				}
				for _, task := range taskList.GetTasks() {
					exercise := db.Exercise{
						Level:       res.Level,
						Type:        res.Type,
						Question:    task.Question,
						Explanation: task.Explanation,
					}
					exercises = append(exercises, exercise)
				}

			case db.ExerciseTypeTranslation:
				taskList, ok := res.GeneratedTaskList.(ai.TranslationTaskList)
				if !ok {
					log.Printf("Failed to convert task list to TranslationTaskList: %v", res.GeneratedTaskList)
					continue
				}
				for _, task := range taskList.GetTasks() {
					exercise := db.Exercise{
						Level:         res.Level,
						Type:          res.Type,
						Question:      task.Russian,
						CorrectAnswer: task.Japanese,
						Explanation:   task.Explanation,
					}
					exercises = append(exercises, exercise)
				}
			case db.ExerciseTypeAudio:
				taskList, ok := res.GeneratedTaskList.(ai.AudioTaskList)
				if !ok {
					log.Printf("Failed to convert task list to AudioTaskList: %v", res.GeneratedTaskList)
					continue
				}

				for _, task := range taskList.GetTasks() {
					exercise := db.Exercise{
						Level:         res.Level,
						Type:          res.Type,
						Question:      task.Question,
						CorrectAnswer: task.Answer,
						AudioText:     task.Text,
						Explanation:   task.Explanation,
					}
					exercises = append(exercises, exercise)
				}
			case db.ExerciseTypeGrammar:
				taskList, ok := res.GeneratedTaskList.(ai.GrammarTaskList)
				if !ok {
					log.Printf("Failed to convert task list to GrammarTaskList: %v", res.GeneratedTaskList)
					continue
				}

				for _, task := range taskList.GetTasks() {
					exercise := db.Exercise{
						Level:    res.Level,
						Type:     res.Type,
						Question: task.Question,
					}
					exercises = append(exercises, exercise)
				}
			case db.ExerciseTypeVocab:
				taskList, ok := res.GeneratedTaskList.(ai.VocabTaskList)
				if !ok {
					log.Printf("Failed to convert task list to VocabTaskList: %v", res.GeneratedTaskList)
					continue
				}

				for _, task := range taskList.GetTasks() {
					exercise := db.Exercise{
						Level:         res.Level,
						Type:          res.Type,
						Question:      task.Russian,
						CorrectAnswer: task.Japanese,
					}
					exercises = append(exercises, exercise)
				}
			}

		}

		if err := j.db.SaveTasksBatch(exercises); err != nil {
			log.Printf("Failed to save tasks from batch: %v", err)
		}
	}
	return nil
}

func (j *job) syncBatchResults() error {
	batchIDs, err := j.db.GetPendingBatches()
	if err != nil {
		return fmt.Errorf("failed to get pending batches: %w", err)
	}

	for _, id := range batchIDs {
		result, ok, err := j.openaiClient.GetBatchResults(id)
		if err != nil {
			log.Printf("Failed to fetch results for batch %s: %v", id, err)
			continue
		}

		if ok {
			var exercises []db.Exercise
			for _, res := range result {
				switch res.Type {
				case db.ExerciseTypeQuestion:
					taskList, ok := res.GeneratedTaskList.(ai.QuestionTaskList)
					if !ok {
						log.Printf("Failed to convert task list to QuestionTaskList: %v", res.GeneratedTaskList)
						continue
					}
					for _, task := range taskList.GetTasks() {
						exercise := db.Exercise{
							Level:       res.Level,
							Type:        res.Type,
							Question:    task.Question,
							Explanation: task.Explanation,
						}
						exercises = append(exercises, exercise)
					}

				case db.ExerciseTypeTranslation:
					taskList, ok := res.GeneratedTaskList.(ai.TranslationTaskList)
					if !ok {
						log.Printf("Failed to convert task list to TranslationTaskList: %v", res.GeneratedTaskList)
						continue
					}
					for _, task := range taskList.GetTasks() {
						exercise := db.Exercise{
							Level:         res.Level,
							Type:          res.Type,
							Question:      task.Russian,
							CorrectAnswer: task.Japanese,
							Explanation:   task.Explanation,
						}
						exercises = append(exercises, exercise)
					}
				}
			}

			if err := j.db.SaveTasksBatch(exercises); err != nil {
				log.Printf("Failed to save tasks from batch %s: %v", id, err)
				continue
			}

			if err := j.db.UpdateBatchStatus(id, "completed"); err != nil {
				log.Printf("Failed to update batch status: %v", err)
			}
		}
	}
	return nil
}

func (j *job) Run(ctx context.Context) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		log.Fatalf("Failed to load Moscow timezone: %v", err)
	}

	//if err := j.generateTasks(); err != nil {
	//	log.Printf("Failed to sync batch results: %v", err)
	//}

	for {
		now := time.Now().In(location)
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, location)

		if now.After(nextRun) {
			nextRun = nextRun.Add(24 * time.Hour)
		}

		waitDuration := time.Until(nextRun)
		log.Printf("Next notification job scheduled at: %v (Moscow Time)", nextRun)

		timer := time.NewTimer(waitDuration)

		select {
		case <-timer.C:
			log.Println("Running notification job...")

			if err := j.generateTasks(); err != nil {
				log.Printf("Failed to generate tasks: %v", err)
			}

			// notify the bot about the new tasks
			users, err := j.db.GetAllUsers()
			if err != nil {
				log.Printf("Failed to get users: %v", err)
				continue
			}

			for _, user := range users {
				if user.Level == "" {
					continue
				}

				unanswered, err := j.db.CountUnsolvedExercisesForUser(user.ID, user.Level)
				if err != nil {
					log.Printf("Failed to count unanswered exercises for user %d: %v", user.ID, err)
					continue
				}

				if unanswered > 0 {
					text := fmt.Sprintf("Доступно %d новых заданий для уровня %s. /task чтобы получить задание.", unanswered, user.Level)
					msg := &telegram.SendMessageParams{
						ChatID: user.TelegramID,
						Text:   text,
					}

					if _, err := j.bot.SendMessage(context.Background(), msg); err != nil {
						log.Printf("Failed to send message to user %d: %v", user.ID, err)
					}

					log.Printf("Sent notification to user: %d", user.ID)
				}
			}
		case <-ctx.Done():
			log.Println("Stopping notification job...")
			timer.Stop()
			return
		}
	}
}
