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
	"os"
	"strings"
	"time"
)

type Client struct {
	client *openai.Client
}

func NewClient(apiKey string) *Client {
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
	)

	return &Client{
		client: &client,
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
	}

	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4o,
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

type BatchTask struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	InputFileID string    `json:"input_file_id"`
	CreatedAt   time.Time `json:"created_at"`
}

func (c *Client) CreateBatchTask(levels []string, types []string, existing map[string][]string, tasksPerLevel int) (*BatchTask, error) {
	ctx := context.Background()

	tempFile, err := os.CreateTemp("", "batch_*.jsonl")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()

	requestCount := 0
	for _, level := range levels {
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
- Грамматическую подсказку и/или по неочевидным или сложным словам

Не используй предложения ниже:
%s
				`, tasksPerLevel, level, existingList)
				schema = GenerateSchema[TranslationTaskList]()

			case db.ExerciseTypeQuestion:
				systemPrompt = "Ты преподаватель японского. Сгенерируй короткие вопросы, на которые можно ответить на японском языке. Они должны быть интересны и полезны для практики."
				userPrompt = fmt.Sprintf(`Сгенерируй %d уникальных вопросов на японском языке уровня %s. Вопросы могут быть о пользователе, повседневной жизни, культуре или языке. Укажи:
- Вопрос на японском
- Эталонный ответ на японском
- Подсказку по грамматике, словам или контексту
Не используй вопросы ниже:
%s
`, tasksPerLevel/2, level, existingList)
				schema = GenerateSchema[QuestionTaskList]()

			default:
				continue
			}

			batchRequest := map[string]interface{}{
				"custom_id": fmt.Sprintf("request-%d-%s-%s", requestCount, exType, level),
				"method":    "POST",
				"url":       openai.BatchNewParamsEndpointV1ChatCompletions,
				"body": map[string]interface{}{
					"model": openai.ChatModelGPT4o,
					"messages": []interface{}{
						map[string]interface{}{
							"role":    "system",
							"content": systemPrompt,
						},
						map[string]interface{}{
							"role":    "user",
							"content": userPrompt,
						},
					},
					"response_format": map[string]interface{}{
						"type": "json_schema",
						"json_schema": map[string]interface{}{
							"name":        "generated_task_list",
							"description": "Новые упражнения для перевода",
							"schema":      schema,
							"strict":      true,
						},
					},
				},
			}

			requestJSON, err := json.Marshal(batchRequest)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal batch request: %w", err)
			}

			if _, err := tempFile.WriteString(string(requestJSON) + "\n"); err != nil {
				return nil, fmt.Errorf("failed to write to temp file: %w", err)
			}

			requestCount++
		}
	}

	_, _ = tempFile.Seek(0, 0)
	file, err := c.client.Files.New(ctx, openai.FileNewParams{
		File:    tempFile,
		Purpose: openai.FilePurposeBatch,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to upload batch file: %w", err)
	}

	batch, err := c.client.Batches.New(ctx, openai.BatchNewParams{
		InputFileID:      file.ID,
		Endpoint:         openai.BatchNewParamsEndpointV1ChatCompletions,
		CompletionWindow: openai.BatchNewParamsCompletionWindow24h,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create batch: %w", err)
	}

	return &BatchTask{
		ID:          batch.ID,
		Status:      string(batch.Status),
		InputFileID: batch.InputFileID,
		CreatedAt:   time.Now(),
	}, nil
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

	batch, err := c.client.Batches.Get(ctx, batchID)
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

	resp, err := c.client.Files.Content(ctx, batch.OutputFileID)
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
