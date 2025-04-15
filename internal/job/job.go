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
}

type OpenAIClient interface {
	CreateBatchTask(levels []string, existing map[string][]string, tasksPerLevel int) (*ai.BatchTask, error)
	GetBatchResults(batchID string) (ai.GeneratedTaskList, bool, error)
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
	existing := make(map[string][]string)

	for _, level := range levels {
		exercises, err := j.db.GetExercisesByLevel(level)
		if err != nil {
			return fmt.Errorf("failed to get exercises: %w", err)
		}

		if len(exercises) == 0 {
			log.Printf("No exercises found for level %s", level)
			continue
		}

		for _, exercise := range exercises {
			existing[level] = append(existing[level], exercise.Russian)
		}
	}

	batch, err := j.openaiClient.CreateBatchTask(levels, existing, 5)
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
			for _, task := range result.Tasks {
				exercise := db.Exercise{
					Russian:         task.Russian,
					CorrectJapanese: task.Japanese,
					GrammarHint:     task.GrammarHint,
					WordHint:        task.WordHint,
					Level:           task.Level,
				}
				exercises = append(exercises, exercise)
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
	if err := j.generateTasks(); err != nil {
		log.Printf("Failed to generate tasks: %v", err)
	}

	if err := j.syncBatchResults(); err != nil {
		log.Printf("Failed to sync batch results: %v", err)
	}

	ticker := time.NewTicker(24 * time.Hour)

	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := j.generateTasks(); err != nil {
				log.Printf("Failed to generate tasks: %v", err)
			}
			if err := j.syncBatchResults(); err != nil {
				log.Printf("Failed to sync batch results: %v", err)
			}
		}
	}
}
