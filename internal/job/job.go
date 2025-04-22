package job

import (
	"context"
	"encoding/json"
	"fmt"
	telegram "github.com/go-telegram/bot"
	"jpbot/internal/ai"
	"jpbot/internal/db"
	"log"
	"os"
	"strconv"
	"strings"
)

type Storager interface {
	SaveTasksBatch(tasks []db.Exercise) error
	SaveWordsBatch(words []db.Word) error
	GetExercisesByLevel(level string) ([]db.Exercise, error)
	GetExercisesByLevelAndType(level, exType string) ([]db.Exercise, error)
	GetAllUsers() ([]db.User, error)
	CountUnsolvedExercisesForUser(userID int64, level string) (int, error)
}

type OpenAIClient interface {
	GetBatchResults(batchID string) ([]ai.OpenAIResponse, bool, error)
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

type WordExampleList struct {
	Examples []db.Example `json:"examples"`
}

type Word struct {
	Index       int     `json:"index"` // это твой word_id
	Kanji       *string `json:"kanji"`
	Kana        string  `json:"kana"`
	Translation string  `json:"translation"`
}

func FindWordByID(filePath string, wordID int) (*Word, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var words []Word
	if err := json.Unmarshal(data, &words); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	for _, w := range words {
		if w.Index == wordID {
			return &w, nil
		}
	}

	return nil, fmt.Errorf("word with id %d not found", wordID)
}

func (j *job) syncWordsTranslationBatchResult(level, batchID string) error {
	result, ok, err := j.openaiClient.GetBatchResults(batchID)
	if err != nil {
		log.Printf("Failed to fetch results for batch %s: %v", batchID, err)
		return err
	}

	if !ok {
		log.Printf("Batch %s not completed yet.", batchID)
		return nil
	}

	type exampleContent struct {
		Examples []struct {
			Sentence []struct {
				Fragment string  `json:"fragment"`
				Furigana *string `json:"furigana"`
			} `json:"sentence"`
			Translation string `json:"translation"`
		} `json:"examples"`
	}

	var words []db.Word

	for _, batchResponse := range result {
		var ex exampleContent
		if err := json.Unmarshal([]byte(batchResponse.Response.Body.Output[0].Content[0].Text), &ex); err != nil {
			log.Printf("Failed to parse example JSON for %s: %v", batchResponse.CustomID, err)
			continue
		}

		parts := strings.Split(batchResponse.CustomID, "word_")
		if len(parts) != 2 {
			log.Printf("Invalid custom_id format: %s", batchResponse.CustomID)
			continue
		}

		wid, _ := strconv.Atoi(parts[1])

		wd, err := FindWordByID(fmt.Sprintf("%s.json", level), wid)

		if err != nil {
			log.Printf("Failed to find word with ID %d: %v", wid, err)
		}

		exmpJSON, err := json.Marshal(ex.Examples)
		if err != nil {
			log.Printf("Failed to marshal examples for word %d: %v", wd.Index, err)
			continue
		}

		var exmpList []db.Example
		if err := json.Unmarshal(exmpJSON, &exmpList); err != nil {
			log.Printf("Failed to unmarshal examples for word %d: %v", wd.Index, err)
			continue
		}

		words = append(words, db.Word{
			Kana:        wd.Kana,
			Kanji:       wd.Kanji,
			Translation: wd.Translation,
			Examples:    exmpList,
			Level:       strings.ToUpper(level),
		})
	}

	if err := j.db.SaveWordsBatch(words); err != nil {
		log.Printf("Failed to save words from batch %s: %v", batchID, err)
		return err
	}
	return nil
}

func (j *job) syncExercises() error {
	levels := []string{db.LevelN5}

	for _, level := range levels {
		// open file questions_level.json
		filePath := fmt.Sprintf("questions_%s.json", level)
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer file.Close()
		// read file, json array of string
		var questions []string
		if err := json.NewDecoder(file).Decode(&questions); err != nil {
			return fmt.Errorf("failed to decode JSON: %w", err)
		}

		// save questions to db
		var exercises []db.Exercise
		for _, question := range questions {
			exercise := db.Exercise{
				Level:   strings.ToUpper(level),
				Type:    db.ExerciseTypeQuestion,
				Content: db.Content{Grammar: question},
			}
			exercises = append(exercises, exercise)
		}

		if err := j.db.SaveTasksBatch(exercises); err != nil {
			return fmt.Errorf("failed to save exercises: %w", err)
		}
		log.Printf("Saved %d exercises for level %s", len(exercises), level)

		// open file audio_level.json
		filePath = fmt.Sprintf("audio_%s.json", level)
		file, err = os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}

		defer file.Close()
		// read file,
		//[
		//  {
		//    "text": "きのう、スーパーへ行きました。りんごとパンを買いました。",
		//    "question": "何を買いましたか？"
		//  },

		type AudioExercise struct {
			Text     string `json:"text"`
			Question string `json:"question"`
		}

		var audioExercises []AudioExercise
		if err := json.NewDecoder(file).Decode(&audioExercises); err != nil {
			return fmt.Errorf("failed to decode JSON: %w", err)
		}

		// save questions to db
		for _, audioExercise := range audioExercises {
			exercise := db.Exercise{
				Level:   strings.ToUpper(level),
				Type:    db.ExerciseTypeAudio,
				Content: db.Content{Grammar: audioExercise.Question},
			}
			exercises = append(exercises, exercise)
		}

	}
}

func (j *job) syncExerciseBatchResult(level, batchID string) error {
	result, ok, err := j.openaiClient.GetBatchResults(batchID)
	if err != nil {
		log.Printf("Failed to fetch results for batch %s: %v", batchID, err)
		return err
	}

	if !ok {
		log.Printf("Batch %s not completed yet.", batchID)
		return nil
	}

	type exampleContent struct {
		Grammar   string `json:"grammar"`
		Meaning   string `json:"meaning"`
		Structure string `json:"structure"`
		Example   string `json:"example"`
	}

	var exercises []db.Exercise

	for _, batchResponse := range result {
		var example exampleContent
		if err := json.Unmarshal([]byte(batchResponse.Response.Body.Output[0].Content[0].Text), &example); err != nil {
			log.Printf("Failed to parse example JSON for %s: %v", batchResponse.CustomID, err)
			continue
		}

		content := db.Content{
			Grammar:   example.Grammar,
			Meaning:   example.Meaning,
			Structure: example.Structure,
			Example:   example.Example,
		}

		exercise := db.Exercise{
			Level:   strings.ToUpper(level),
			Type:    db.ExerciseTypeGrammar,
			Content: content,
		}

		exercises = append(exercises, exercise)
	}

	if err := j.db.SaveTasksBatch(exercises); err != nil {
		log.Printf("Failed to save words from batch %s: %v", batchID, err)
		return err
	}

	return nil
}

func (j *job) Run(ctx context.Context) {
	//if err := j.generateTasks(); err != nil {
	//	log.Printf("Failed to sync batch results: %v", err)
	//}

	//wordsBatches := map[string]string{
	//	"n5": "batch_6805b3554e908190a88ba9884f4920a2",
	//	"n4": "batch_6805bf9739c481908d50cbb99ce43851",
	//	"n3": "batch_6805bf9ece5081908db730057347c555",
	//}
	//
	//for level, batchID := range wordsBatches {
	//	if err := j.syncWordsTranslationBatchResult(level, batchID); err != nil {
	//		log.Printf("Failed to sync batch results: %v", err)
	//	}
	//}

	//grammarBatches := map[string]string{
	//	"n5": "batch_68065a7dae448190974adbfa232fae3d",
	//}
	//
	//for level, batchID := range grammarBatches {
	//	if err := j.syncExerciseBatchResult(level, batchID); err != nil {
	//		log.Printf("Failed to sync batch results: %v", err)
	//	}
	//}
}
