package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"io"
	"jpbot/internal/db"
	"log"
	"strings"
	"time"
)

type Client struct {
	grokClient   *openai.Client
	openaiClient *openai.Client
}

func NewClient(grokApiKey, openAIApiKey string) *Client {
	client := openai.NewClient(
		option.WithAPIKey(grokApiKey),
		option.WithBaseURL("https://api.x.ai/v1"),
	)

	openAIClient := openai.NewClient(
		option.WithAPIKey(openAIApiKey),
	)

	return &Client{
		grokClient:   &client,
		openaiClient: &openAIClient,
	}
}

type ExerciseFeedback struct {
	Score      int    `json:"score" jsonschema_description:"Оценка ответа от 0 до 100"`
	Feedback   string `json:"feedback" jsonschema_description:"Ошибки или комментарии к ответу"`
	Suggestion string `json:"suggestion" jsonschema_description:"Улучшенный ответ"`
}

func GenerateSchema[T any]() interface{} {
	// Structured Outputs uses a subset of JSON schema
	// These flags are necessary to comply with the subset
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}

func (c *Client) CheckExercise(submission db.Submission) (ExerciseFeedback, error) {
	ctx := context.Background()
	schema := GenerateSchema[ExerciseFeedback]()

	var systemPrompt, userPrompt string

	switch submission.Exercise.Type {
	case db.ExerciseTypeTranslation:
		systemPrompt = "Ты преподаватель японского языка. Проверь перевод с русского на японский по точности, грамматике и естественности. Ответь кратко в формате JSON: оценка (0-100), комментарий (ошибки или пояснение), улучшенный вариант."
		userPrompt = fmt.Sprintf(`Оригинал: "%s"
Перевод: 「%s」
Правильный: 「%s」`,
			submission.Exercise.Question,
			submission.UserInput,
			submission.Exercise.CorrectAnswer,
		)

	case db.ExerciseTypeQuestion:
		systemPrompt = "Ты преподаватель японского языка. Проверь ответ на вопрос по точности, грамматике и естественности. Ответь кратко в формате JSON: оценка (0-100), комментарий (ошибки или пояснение), улучшенный вариант."
		userPrompt = fmt.Sprintf(`Вопрос: "%s"
Ответ: 「%s」`,
			submission.Exercise.Question,
			submission.UserInput,
		)
	case db.ExerciseTypeAudio:
		systemPrompt = "Ты преподаватель японского языка. Проверь правильно ли ответили на вопрос по тексту. Ответь кратко в формате JSON: оценка (0-100), комментарий (ошибки или пояснение), улучшенный вариант."
		userPrompt = fmt.Sprintf(`Текст: "%s"
Вопрос: "%s"
Ответ: 「%s」`,
			submission.Exercise.AudioText,
			submission.Exercise.Question,
			submission.UserInput,
		)
	case db.ExerciseTypeGrammar:
		systemPrompt = "Ты преподаватель японского языка. Проверь, правильно ли пользователь использовал указанную грамматическую конструкцию в своем предложении. Ответь кратко в формате JSON: оценка (0-100), комментарий (ошибки или пояснение), улучшенный вариант (если есть ошибки)."
		userPrompt = fmt.Sprintf(`Грамматическая конструкция: "%s"
Пользовательский ответ: 「%s」`,
			submission.Exercise.Question,
			submission.UserInput,
		)
	case db.ExerciseTypeVocab:
		systemPrompt = "Ты преподаватель японского языка. Проверь правильность перевода слова с русского на японский. Ответь кратко в формате JSON: оценка (0-100), комментарий (пример использования слова, какие-то особенности), улучшенный вариант (если есть ошибки).　Если нет перевода, то просто дай подсказку."
		userPrompt = fmt.Sprintf(`Русское слово: "%s"
Пользовательский Перевод: 「%s」
Правильный перевод: 「%s」`,
			submission.Exercise.Question,
			submission.UserInput,
			submission.Exercise.CorrectAnswer,
		)
	default:
		return ExerciseFeedback{}, fmt.Errorf("unknown exercise type: %s", submission.Exercise.Type)
	}

	resp, err := c.grokClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: "grok-3-fast-beta",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "exercise_feedback",
					Description: openai.String("Обратная связь по переводу с японского"),
					Schema:      schema,
					Strict:      openai.Bool(true),
				},
			},
		},
	})

	if err != nil {
		return ExerciseFeedback{}, fmt.Errorf("GPT request failed: %w", err)
	}

	var feedback ExerciseFeedback

	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &feedback); err != nil {
		return feedback, fmt.Errorf("failed to parse GPT response: %w", err)
	}

	return feedback, nil
}

type TranslationTask struct {
	Russian     string `json:"russian" jsonschema_description:"Предложение на русском языке"`
	Japanese    string `json:"japanese" jsonschema_description:"Перевод на японском языке"`
	Explanation string `json:"explanation" jsonschema_description:"Подсказка по грамматике или сложным словам"`
}

type QuestionTask struct {
	Question    string `json:"question" jsonschema_description:"Вопрос на японском языке"`
	Explanation string `json:"explanation" jsonschema_description:"Подсказка по грамматике, словам или контексту"`
}

type AudioTask struct {
	Text        string `json:"text" jsonschema_description:"Короткий текст на японском языке (1-3 предложения)"`
	Question    string `json:"question" jsonschema_description:"Вопрос по содержанию текста на японском языке"`
	Answer      string `json:"answer" jsonschema_description:"Правильный ответ на вопрос"`
	Explanation string `json:"explanation" jsonschema_description:"Подсказка по грамматике, словам или контексту"`
}

type VocabTask struct {
	Russian  string `json:"russian" jsonschema_description:"Русское слово"`
	Japanese string `json:"japanese" jsonschema_description:"Перевод на японском языке"`
}

type GrammarTask struct {
	Question string `json:"question" jsonschema_description:"Грамматическая конструкция и ее использование"`
}

type QuestionTaskList struct {
	Tasks []QuestionTask `json:"tasks" jsonschema_description:"Список сгенерированных заданий"`
}

func (t QuestionTaskList) GetTasks() []QuestionTask {
	return t.Tasks
}

type TranslationTaskList struct {
	Tasks []TranslationTask `json:"tasks" jsonschema_description:"Список сгенерированных заданий"`
}

func (t TranslationTaskList) GetTasks() []TranslationTask {
	return t.Tasks
}

type AudioTaskList struct {
	Tasks []AudioTask `json:"tasks" jsonschema_description:"Список сгенерированных заданий"`
}

func (t AudioTaskList) GetTasks() []AudioTask {
	return t.Tasks
}

type GrammarTaskList struct {
	Tasks []GrammarTask `json:"tasks" jsonschema_description:"Список сгенерированных заданий"`
}

func (t GrammarTaskList) GetTasks() []GrammarTask {
	return t.Tasks
}

type VocabTaskList struct {
	Tasks []VocabTask `json:"tasks" jsonschema_description:"Список сгенерированных заданий"`
}

func (t VocabTaskList) GetTasks() []VocabTask {
	return t.Tasks
}

type BatchTask struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	InputFileID string    `json:"input_file_id"`
	CreatedAt   time.Time `json:"created_at"`
}

func (c *Client) CreateBatchTask(level string, types []string, existing map[string][]string, tasksPerLevel int) ([]BatchResult, error) {
	ctx := context.Background()
	var results []BatchResult
	requestCount := 0

	for _, exType := range types {
		key := fmt.Sprintf("%s|%s", exType, level)
		existingList := ""
		for _, e := range existing[key] {
			existingList += fmt.Sprintf("- %s\n", e)
		}

		var systemPrompt, userPrompt string
		var schema any

		switch exType {
		case db.ExerciseTypeTranslation:
			systemPrompt = "Ты преподаватель японского языка. Твоя задача — создать простые и полезные упражнения на перевод с русского на японский язык. Каждое упражнение должно содержать грамматическую подсказку и ключевое слово с объяснением."
			userPrompt = fmt.Sprintf(`Сгенерируй %d уникальных предложений уровня %s. Укажи:
- Оригинал на русском
- Перевод на японский
- Краткий разбор предложения, включающий:
  - Значение каждого ключевого слова или фразы (с ромадзи для чтения).
  - Грамматические конструкции (например, частицы, формы глаголов).
  - Роль каждого элемента в предложении (например, наречие времени, объектный показатель).
Не используй предложения ниже:
%s
`, tasksPerLevel, level, existingList)
			schema = GenerateSchema[TranslationTaskList]()

		case db.ExerciseTypeQuestion:
			systemPrompt = "Ты преподаватель японского. Сгенерируй короткие вопросы, на которые можно ответить на японском языке. Они должны быть интересны и полезны для практики."
			userPrompt = fmt.Sprintf(`Сгенерируй %d уникальных вопросов на японском языке уровня %s. Вопросы могут быть о пользователе, повседневной жизни, культуре или языке. Укажи:
- Вопрос на японском
- Краткий разбор вопроса, включающий:
  - Значение каждого ключевого слова или фразы (с ромадзи для чтения).
  - Грамматические конструкции (например, частицы, формы глаголов).
  - Роль каждого элемента в предложении (например, наречие времени, объектный показатель).
Не используй вопросы ниже:
%s
`, tasksPerLevel, level, existingList)
			schema = GenerateSchema[QuestionTaskList]()
		case db.ExerciseTypeAudio:
			systemPrompt = "Ты преподаватель японского языка. Сгенерируй аудиозадания для практики аудирования. Каждое задание включает короткий текст на японском языке (для аудио), вопрос по содержанию текста, правильный ответ и объяснение."
			userPrompt = fmt.Sprintf(`Сгенерируй %d уникальных аудиозаданий уровня %s. Укажи:
- Короткий текст на японском (1-3 предложения, подходящие для аудирования).
- Вопрос по содержанию текста на японском.
- Правильный ответ на вопрос.
- Краткий разбор, включающий:
  - Значение каждого ключевого слова или фразы (с ромадзи для чтения).
  - Грамматические конструкции (например, частицы, формы глаголов).
  - Роль каждого элемента в предложении.
Не используй задания ниже:
%s
`, tasksPerLevel, level, existingList)
			schema = GenerateSchema[AudioTaskList]()
		case db.ExerciseTypeGrammar:
			systemPrompt = "Ты преподаватель японского языка. Твоя задача — объяснить грамматические конструкции и их использование."
			userPrompt = fmt.Sprintf(`Сгенерируй %d уникальных описаний грамматических конструкций уровня %s. Укажи:
- Грамматическую конструкцию (например, ～たい, ～ながら, ～てしまう) и ее использование.
Не используй грамматические конструкции ниже:
%s
`, tasksPerLevel, level, existingList)
			schema = GenerateSchema[GrammarTaskList]()
		case db.ExerciseTypeVocab:
			systemPrompt = "Ты преподаватель японского языка. Сгенерируй задания на перевод японских слов на русский язык. Используй ежедневные популярные слова. Это может быть любая часть речи, включая существительные, глаголы и прилагательные."
			userPrompt = fmt.Sprintf(`Сгенерируй %d уникальных заданий на перевод японских слов. Укажи:
- Русское слово
- Перевод на японском

Не используй слова ниже:
%s
`, tasksPerLevel, existingList)
			schema = GenerateSchema[VocabTaskList]()
		default:
			log.Printf("Unknown exercise type: %s", exType)
			continue
		}

		log.Printf("Sending request for %s-%s", exType, level)

		// Make a synchronous API call instead of creating a batch
		resp, err := c.grokClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model: "grok-3-beta",
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(systemPrompt),
				openai.UserMessage(userPrompt),
			},
			ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
					JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
						Name:        "generated_task_list",
						Description: openai.String("Новые упражнения для перевода"),
						Schema:      schema,
						Strict:      openai.Bool(true),
					},
				},
			},
		})

		log.Printf("Received response for %s-%s", exType, level)

		if err != nil {
			return nil, fmt.Errorf("failed to generate tasks for %s-%s: %w", exType, level, err)
		}

		// Parse the response
		var result BatchResult
		result.Type = exType
		result.Level = level

		switch exType {
		case db.ExerciseTypeTranslation:
			var taskList TranslationTaskList
			if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &taskList); err != nil {
				return nil, fmt.Errorf("failed to parse translation task list for %s-%s: %w", exType, level, err)
			}
			result.GeneratedTaskList = taskList

		case db.ExerciseTypeQuestion:
			var taskList QuestionTaskList
			if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &taskList); err != nil {
				return nil, fmt.Errorf("failed to parse question task list for %s-%s: %w", exType, level, err)
			}
			result.GeneratedTaskList = taskList
		case db.ExerciseTypeAudio:
			var taskList AudioTaskList
			if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &taskList); err != nil {
				return nil, fmt.Errorf("failed to parse audio task list for %s-%s: %w", exType, level, err)
			}
			result.GeneratedTaskList = taskList
		case db.ExerciseTypeGrammar:
			var taskList GrammarTaskList
			if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &taskList); err != nil {
				return nil, fmt.Errorf("failed to parse grammar task list for %s-%s: %w", exType, level, err)
			}
			result.GeneratedTaskList = taskList
		case db.ExerciseTypeVocab:
			var taskList VocabTaskList
			if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &taskList); err != nil {
				return nil, fmt.Errorf("failed to parse vocab task list for %s-%s: %w", exType, level, err)
			}
			result.GeneratedTaskList = taskList
		default:
			log.Printf("Unknown exercise type in response: %s", exType)
			continue
		}

		results = append(results, result)
		requestCount++
	}

	return results, nil
}

func (c *Client) GenerateAudio(text string) (io.ReadCloser, error) {
	resp, err := c.openaiClient.Audio.Speech.New(context.TODO(), openai.AudioSpeechNewParams{
		Input:          text,
		Model:          openai.SpeechModelGPT4oMiniTTS,
		Voice:          openai.AudioSpeechNewParamsVoiceFable,
		ResponseFormat: openai.AudioSpeechNewParamsResponseFormatOpus,
		Speed:          openai.Float(0.75),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate audio: %w", err)
	}
	return resp.Body, nil
}

func getLevelAndTypeFromCustomID(customID string) (string, string) {
	parts := strings.Split(customID, "-")
	if len(parts) < 3 {
		return "", ""
	}

	level := parts[len(parts)-1]
	exType := parts[len(parts)-2]

	return level, exType
}

type GeneratedTaskList[T any] interface {
	GetTasks() []T
}

type BatchResult struct {
	Type              string      `json:"type"`
	Level             string      `json:"level"`
	GeneratedTaskList interface{} `json:"generated_task_list"` // Используем interface{} для гибкости
}

func (c *Client) GetBatchResults(batchID string) ([]BatchResult, bool, error) {
	ctx := context.Background()

	var results []BatchResult

	batch, err := c.grokClient.Batches.Get(ctx, batchID)
	if err != nil {
		return results, false, fmt.Errorf("failed to retrieve batch: %w", err)
	}

	if batch.Status != openai.BatchStatusCompleted {
		log.Printf("Batch %s is not completed yet. Status: %s", batch.ID, batch.Status)
		return results, false, nil
	}

	if batch.OutputFileID == "" {
		return results, false, fmt.Errorf("no output file available")
	}

	resp, err := c.grokClient.Files.Content(ctx, batch.OutputFileID)
	if err != nil {
		return results, false, fmt.Errorf("failed to get file content: %w", err)
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return results, false, fmt.Errorf("failed to read file content: %w", err)
	}

	lines := bytes.Split(content, []byte("\n"))

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		var batchResponse struct {
			CustomID string `json:"custom_id"`
			Response struct {
				Body struct {
					Choices []struct {
						Message struct {
							Content string `json:"content"`
						} `json:"message"`
					} `json:"choices"`
				} `json:"body"`
			} `json:"response"`
		}

		if err := json.Unmarshal(line, &batchResponse); err != nil {
			return results, false, fmt.Errorf("failed to parse batch response: %w", err)
		}

		level, exType := getLevelAndTypeFromCustomID(batchResponse.CustomID)

		var result BatchResult
		result.Type = exType
		result.Level = level

		switch exType {
		case db.ExerciseTypeTranslation:
			var taskList TranslationTaskList
			if err := json.Unmarshal([]byte(batchResponse.Response.Body.Choices[0].Message.Content), &taskList); err != nil {
				return results, false, fmt.Errorf("failed to parse translation task list: %w", err)
			}
			result.GeneratedTaskList = taskList // Присваиваем напрямую как interface{}

		case db.ExerciseTypeQuestion:
			var taskList QuestionTaskList
			if err := json.Unmarshal([]byte(batchResponse.Response.Body.Choices[0].Message.Content), &taskList); err != nil {
				return results, false, fmt.Errorf("failed to parse question task list: %w", err)
			}
			result.GeneratedTaskList = taskList // Присваиваем напрямую как interface{}

		default:
			log.Printf("Unknown exercise type: %s", exType)
			continue
		}

		results = append(results, result)
	}

	return results, true, nil
}
