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
		filePath := fmt.Sprintf("materials/vocab_%s.json", strings.ToLower(level))
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

type exerciseFile struct {
	filePath    string
	exType      string
	contentType interface{}
}

func (j *job) processExerciseFile(level string, ef exerciseFile) ([]db.Exercise, error) {
	file, err := os.Open(ef.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", ef.filePath, err)
	}
	defer file.Close()

	var rawExercises []interface{}
	if err := json.NewDecoder(file).Decode(&rawExercises); err != nil {
		return nil, fmt.Errorf("failed to decode JSON from %s: %w", ef.filePath, err)
	}

	var exercises []db.Exercise
	for _, raw := range rawExercises {
		contentJSON, err := json.Marshal(raw)
		if err != nil {
			log.Printf("Failed to marshal content for %v: %v", raw, err)
			continue
		}

		exercises = append(exercises, db.Exercise{
			Level:   strings.ToUpper(level),
			Type:    ef.exType,
			Content: contentJSON,
		})
	}

	if err := j.db.SaveTasksBatch(exercises); err != nil {
		return nil, fmt.Errorf("failed to save %s exercises: %w", ef.exType, err)
	}

	log.Printf("Saved %d %s exercises for level %s", len(exercises), ef.exType, level)
	return exercises, nil
}

func (j *job) syncExercises() error {
	levels := []string{db.LevelN5}
	exerciseFiles := []exerciseFile{
		{
			filePath:    "materials/questions_%s.json",
			exType:      db.ExerciseTypeQuestion,
			contentType: db.QuestionContent{},
		},
		{
			filePath: "materials/audio_%s.json",
			exType:   db.ExerciseTypeAudio,
			contentType: struct {
				Text     string `json:"text"`
				Question string `json:"question"`
			}{},
		},
		{
			filePath: "materials/sentences_%s.json",
			exType:   db.ExerciseTypeTranslation,
			contentType: struct {
				Russian  string `json:"russian"`
				Japanese string `json:"japanese"`
			}{},
		},
		{
			filePath: "materials/grammar_%s.json",
			exType:   db.ExerciseTypeGrammar,
			contentType: struct {
				Grammar   string `json:"grammar"`
				Meaning   string `json:"meaning"`
				Structure string `json:"structure"`
				Example   string `json:"example"`
			}{},
		},
	}

	var allExercises []db.Exercise
	for _, level := range levels {
		for _, ef := range exerciseFiles {
			filePath := fmt.Sprintf(ef.filePath, strings.ToLower(level))
			exercises, err := j.processExerciseFile(level, exerciseFile{
				filePath:    filePath,
				exType:      ef.exType,
				contentType: ef.contentType,
			})
			if err != nil {
				return err
			}
			allExercises = append(allExercises, exercises...)
		}
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
