package job

import (
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
}

type OpenAIClient interface {
	CreateBatchTask(levels, types []string, existing map[string][]string, tasksPerLevel int) (*ai.BatchTask, error)
	GetBatchResults(batchID string) ([]ai.BatchResult, bool, error)
}

type job struct {
	bot          *telegram.Bot
	db           Storager
	openaiClient OpenAIClient
}

func NewJob(db Storager, openaiClient *ai.Client) *job {
	return &job{
		db:           db,
		openaiClient: openaiClient,
	}
}

func (j *job) generateTasks() error {
	levels := []string{db.LevelN3, db.LevelN4, db.LevelN5, db.LevelN2, db.LevelN1, db.LevelBeginner}
	types := []string{db.ExerciseTypeQuestion, db.ExerciseTypeTranslation}
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

	batch, err := j.openaiClient.CreateBatchTask(levels, types, existing, 5)
	if err != nil {
		log.Fatalf("Failed to generate tasks: %v", err)
	}

	log.Printf("Generated task with ID: %s", batch.ID)

	if err := j.db.SaveBatchMeta(batch.ID, "in_progress"); err != nil {
		log.Printf("Failed to save batch meta: %v", err)
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

func (j *job) Run() {
	if err := j.syncBatchResults(); err != nil {
		log.Printf("Failed to sync batch results: %v", err)
	}

	if err := j.generateTasks(); err != nil {
		log.Printf("Failed to generate tasks: %v", err)
	}

	ticker := time.NewTicker(24 * time.Hour)

	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := j.syncBatchResults(); err != nil {
				log.Printf("Failed to sync batch results: %v", err)
			}
			if err := j.generateTasks(); err != nil {
				log.Printf("Failed to generate tasks: %v", err)
			}
		}
	}
}
