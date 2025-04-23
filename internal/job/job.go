package job

import (
	"context"
	"encoding/json"
	"fmt"
	telegram "github.com/go-telegram/bot"
	"jpbot/internal/db"
	"log"
	"os"
	"strings"
)

type Storager interface {
	SaveTasksBatch(tasks []db.Exercise) error
	SaveWordsBatch(words []db.Word) error
	GetExercisesByLevel(level string) ([]db.Exercise, error)
	GetExercisesByLevelAndType(level, exType string) ([]db.Exercise, error)
	GetAllUsers() ([]db.User, error)
	CountUnsolvedExercisesForUser(userID int64, level string) (int, error)
	IsExercisesInitialized() (bool, error)
	IsWordsInitialized() (bool, error)
}

type job struct {
	bot *telegram.Bot
	db  Storager
}

func NewJob(db Storager, bot *telegram.Bot) *job {
	return &job{
		bot: bot,
		db:  db,
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

func (j *job) syncWords() error {
	levels := []string{db.LevelN5}
	var words []db.Word

	for _, level := range levels {
		filePath := fmt.Sprintf("materials/vocab_%s.json", level)
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}

		defer file.Close()

		type Vocab struct {
			Kanji       string `json:"kanji"`
			Kana        string `json:"kana"`
			Translation string `json:"translation"`
			Examples    []struct {
				Sentence []struct {
					Fragment string  `json:"fragment"`
					Furigana *string `json:"furigana"`
				} `json:"sentence"`
				Translation string `json:"translation"`
			} `json:"examples"`
			Level string `json:"level"`
		}

		var vocabList []Vocab

		if err := json.NewDecoder(file).Decode(&vocabList); err != nil {
			return fmt.Errorf("failed to decode JSON: %w", err)
		}

		for _, vocab := range vocabList {
			var kanji *string
			if vocab.Kanji != "" {
				kanji = &vocab.Kanji
			} else {
				kanji = nil
			}

			examples := make([]db.Example, len(vocab.Examples))
			for i, example := range vocab.Examples {
				var sentence []db.Sentence
				for _, fragment := range example.Sentence {
					sentence = append(sentence, db.Sentence{
						Fragment: fragment.Fragment,
						Furigana: fragment.Furigana,
					})
				}
				examples[i] = db.Example{
					Sentence:    sentence,
					Translation: example.Translation,
				}
			}

			words = append(words, db.Word{
				Kana:        vocab.Kana,
				Kanji:       kanji,
				Translation: vocab.Translation,
				Examples:    examples,
				Level:       strings.ToUpper(level),
			})
		}
	}

	if err := j.db.SaveWordsBatch(words); err != nil {
		return fmt.Errorf("failed to save words: %w", err)
	}

	return nil
}

func (j *job) syncExercises() error {
	levels := []string{db.LevelN5}

	for _, level := range levels {
		filePath := fmt.Sprintf("materials/questions_%s.json", level)
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}

		defer file.Close()
		var questions []string
		if err := json.NewDecoder(file).Decode(&questions); err != nil {
			return fmt.Errorf("failed to decode JSON: %w", err)
		}

		var exercises []db.Exercise
		for _, question := range questions {
			content := db.QuestionContent{
				Question: question,
			}

			contentJSON, err := json.Marshal(content)
			if err != nil {
				log.Printf("Failed to marshal content for %s: %v", question, err)
				continue
			}

			exercise := db.Exercise{
				Level:   strings.ToUpper(level),
				Type:    db.ExerciseTypeQuestion,
				Content: contentJSON,
			}
			exercises = append(exercises, exercise)
		}

		if err := j.db.SaveTasksBatch(exercises); err != nil {
			return fmt.Errorf("failed to save exercises: %w", err)
		}
		log.Printf("Saved %d exercises for level %s", len(exercises), level)

		filePath = fmt.Sprintf("materials/audio_%s.json", level)
		file, err = os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}

		defer file.Close()

		type AudioExercise struct {
			Text     string `json:"text"`
			Question string `json:"question"`
		}

		var audioExercises []AudioExercise
		if err := json.NewDecoder(file).Decode(&audioExercises); err != nil {
			return fmt.Errorf("failed to decode JSON: %w", err)
		}

		for _, audioExercise := range audioExercises {
			content := db.AudioContent{
				Text:     audioExercise.Text,
				Question: audioExercise.Question,
			}

			contentJSON, err := json.Marshal(content)
			if err != nil {
				log.Printf("Failed to marshal content for %s: %v", audioExercise.Question, err)
				continue
			}

			exercise := db.Exercise{
				Level:   strings.ToUpper(level),
				Type:    db.ExerciseTypeAudio,
				Content: contentJSON,
			}
			exercises = append(exercises, exercise)
		}
		if err := j.db.SaveTasksBatch(exercises); err != nil {
			return fmt.Errorf("failed to save exercises: %w", err)
		}

		log.Printf("Saved %d audio exercises for level %s", len(exercises), level)

		filePath = fmt.Sprintf("materials/sentences_%s.json", level)
		file, err = os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}

		defer file.Close()
		type SentenceExercise struct {
			Russian  string `json:"russian"`
			Japanese string `json:"japanese"`
		}
		var sentenceExercises []SentenceExercise
		if err := json.NewDecoder(file).Decode(&sentenceExercises); err != nil {
			return fmt.Errorf("failed to decode JSON: %w", err)
		}

		for _, sentenceExercise := range sentenceExercises {
			content := db.SentenceContent{
				Russian:  sentenceExercise.Russian,
				Japanese: sentenceExercise.Japanese,
			}

			contentJSON, err := json.Marshal(content)
			if err != nil {
				log.Printf("Failed to marshal content for %s: %v", sentenceExercise.Japanese, err)
				continue
			}

			exercise := db.Exercise{
				Level:   strings.ToUpper(level),
				Type:    db.ExerciseTypeTranslation,
				Content: contentJSON,
			}
			exercises = append(exercises, exercise)
		}

		if err := j.db.SaveTasksBatch(exercises); err != nil {
			return fmt.Errorf("failed to save exercises: %w", err)
		}

		log.Printf("Saved %d translation exercises for level %s", len(exercises), level)
	}

	return nil
}

func (j *job) Run(ctx context.Context) {
	isExercisesInitialized, err := j.db.IsExercisesInitialized()
	if err != nil {
		log.Printf("Failed to check if exercises are initialized: %v", err)
		return
	}

	if !isExercisesInitialized {
		log.Println("Exercises not initialized. Syncing...")
		if err := j.syncExercises(); err != nil {
			log.Printf("Failed to sync exercises: %v", err)
		}
	}

	wordInitialized, err := j.db.IsWordsInitialized()

	if err != nil {
		log.Printf("Failed to check if words are initialized: %v", err)
		return
	}

	if !wordInitialized {
		if err := j.syncWords(); err != nil {
			log.Printf("Failed to sync words: %v", err)
		}
	}
}
