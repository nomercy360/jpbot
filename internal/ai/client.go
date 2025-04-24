package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"io"
	"jpbot/internal/db"
)

type Client struct {
	grokClient   *openai.Client
	openaiClient *openai.Client
}

const (
	ChatGPTModel = "gpt-4.1-mini-2025-04-14"
)

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
	Score      int    `json:"score" jsonschema_description:"Числовая оценка, отражающая качество ответа или примера."`
	Comment    string `json:"feedback" jsonschema_description:"Комментарий с объяснением оценки или замечаниями."`
	Suggestion string `json:"suggestion,nullable" jsonschema_description:"Предложение по улучшению или исправлению."`
}

func GenerateSchema[T any]() interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}

type WordTranslationEvaluation struct {
	Score   int    `json:"score" jsonschema_description:"Оценка от 0 до 100, отражающая точность перевода."`
	Comment string `json:"comment,nullable" jsonschema_description:"Комментарий с объяснением оценки или null, если перевод корректен."`
}

func (c *Client) CheckWordTranslation(word, translation, userInput string) (WordTranslationEvaluation, error) {
	ctx := context.Background()

	systemPrompt := `Ты преподаватель японского языка. Проверь правильность перевода слова с русского на японский. Оцени перевод по 100-балльной шкале. Если перевод неверный или неполный, добавь краткий комментарий (1–2 предложения), объясняющий, в чём ошибка: — например, неверная часть речи, неточность значения, опущена важная часть, используется другое слово. Если перевод корректен — комментарий должен быть 'null'. Формат ответа: - оценка: (целое число от 0 до 100) - комментарий: (строка или 'null')`

	examples := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(`Русское слово: "еда"
Пользовательский перевод: 「食べ物」
Правильный перевод: 「食べ物」`),
		openai.AssistantMessage(`{
  "score": 100,
  "comment": null
}`),

		openai.UserMessage(`Русское слово: "поход"
Пользовательский перевод: 「道」
Правильный перевод: 「ハイキング」`),
		openai.AssistantMessage(`{
  "score": 40,
  "comment": "Перевод неточный: 「道」(みち) означает 'дорога', а не 'поход' в смысле прогулки или похода в горы."
}`),

		openai.UserMessage(`Русское слово: "работать"
Пользовательский перевод: 「仕事」
Правильный перевод: 「働く」`),
		openai.AssistantMessage(`{
  "score": 50,
  "comment": "Перевод неверный: 「仕事」(しごと) — это существительное 'работа', а не глагол 'работать'."
}`),
	}

	userPrompt := fmt.Sprintf(`Русское слово: "%s"
Пользовательский перевод: 「%s」
Правильный перевод: 「%s」`,
		translation,
		userInput,
		word,
	)

	messages := append(
		[]openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
		},
		examples...,
	)

	messages = append(messages, openai.UserMessage(userPrompt))

	schema := GenerateSchema[WordTranslationEvaluation]()

	resp, err := c.openaiClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    ChatGPTModel,
		Messages: messages,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "translation_evaluation",
					Schema: schema,
					Strict: openai.Bool(true),
				},
			},
		},
	})

	if err != nil {
		return WordTranslationEvaluation{}, fmt.Errorf("GPT request failed: %w", err)
	}

	var explanation WordTranslationEvaluation
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &explanation); err != nil {
		return explanation, fmt.Errorf("failed to parse GPT response: %w", err)
	}

	return explanation, nil
}

func (c *Client) CheckExercise(submission db.Submission) (ExerciseFeedback, error) {
	ctx := context.Background()
	schema := GenerateSchema[ExerciseFeedback]()

	var systemPrompt, userPrompt string
	var messages []openai.ChatCompletionMessageParamUnion

	switch submission.Exercise.Type {
	case db.ExerciseTypeTranslation:
		content, _ := db.ContentAs[db.SentenceContent](submission.Exercise.Content)
		systemPrompt = `Ты преподаватель японского языка. Проверь перевод с русского на японский по точности, грамматике и естественности. Укажи:
1.	score: оценка от 0 до 100
2.	comment: краткое объяснение (до 1–2 предложений)
3.	suggestion: улучшенный вариант предложения и его перевод, если перевод некорректен; иначе — null`

		userPrompt = fmt.Sprintf(`Оригинал: "%s"
Перевод: 「%s」
Правильный: 「%s」`,
			content.Russian,
			submission.UserInput,
			content.Japanese,
		)
		messages = append(messages,
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		)

	case db.ExerciseTypeQuestion:
		content, _ := db.ContentAs[db.QuestionContent](submission.Exercise.Content)

		systemPrompt = `Ты преподаватель японского языка. Проверь, правильно ли ученик ответил на вопрос. Укажи:
1.	score: оценка от 0 до 100
2.	comment: краткое объяснение (до 1–2 предложений)
3.	suggestion: улучшенный вариант ответа и его перевод, если есть ошибки; иначе — null`

		userPrompt = fmt.Sprintf(`Вопрос: "%s"
Ответ: 「%s」`,
			content.Question,
			submission.UserInput,
		)
		messages = append(messages,
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		)

	case db.ExerciseTypeAudio:
		content, _ := db.ContentAs[db.AudioContent](submission.Exercise.Content)

		systemPrompt = `Ты преподаватель японского языка. Проверь, правильно ли ученик ответил на вопрос по тексту. Укажи:
1.	score: оценка от 0 до 100
2.	comment: краткое объяснение (до 1–2 предложений)
3.	suggestion: улучшенный вариант ответа и его перевод, если есть ошибки; иначе — null`

		userPrompt = fmt.Sprintf(`Текст: "%s"
Вопрос: "%s"
Ответ: 「%s」`,
			content.Text,
			content.Question,
			submission.UserInput,
		)
		messages = append(messages,
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		)

	case db.ExerciseTypeGrammar:
		systemPrompt = `Ты — преподаватель японского языка. Проверь, правильно ли ученик использовал грамматическую конструкцию в предложении. Укажи:
	1.	score: оценка от 0 до 100
	2.	comment: краткое объяснение (до 1–2 предложений)
	3.	suggestion: только исправленное предложение и его перевод, без пояснений`
		userPrompt = fmt.Sprintf(`Грамматика: %s
Пример ученика: %s`,
			submission.Exercise.Content,
			submission.UserInput,
		)
		examples := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(`Грамматика: 〜たい
Пример ученика: 日本へ行きたいです。`),
			openai.AssistantMessage(`{
  "score": 90,
  "comment": "Предложение грамматически верное и звучит естественно.",
  "suggestion": null
}`),
			openai.UserMessage(`Грамматика: 〜てもいいです
Пример ученика: 食べてもいいと思います。`),
			openai.AssistantMessage(`{
"score": 80,
"comment": "Грамматика используется правильно, хотя '〜てもいい' обычно выражает разрешение, а не мнение.",
"suggestion": null
}`),
			openai.UserMessage(`Грамматика: 〜ましょうか
Пример ученика: 宿題をしましょうかね。`),
			openai.AssistantMessage(`{
"score": 60,
"comment": "Фраза выглядит как внутренняя речь, но ましょうか обычно используется в вопросе к собеседнику. Частица ね делает её неестественной.",
"suggestion": "宿題をしましょうか？"
}`),
			openai.UserMessage(userPrompt),
		}
		messages = append(messages, examples...)
	default:
		return ExerciseFeedback{}, fmt.Errorf("unknown exercise type: %s", submission.Exercise.Type)
	}

	resp, err := c.openaiClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    ChatGPTModel,
		Messages: messages,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "feedback",
					Schema: schema,
					Strict: openai.Bool(true),
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

func (c *Client) GenerateAudio(text string) (io.ReadCloser, error) {
	resp, err := c.openaiClient.Audio.Speech.New(context.TODO(), openai.AudioSpeechNewParams{
		Input:          text,
		Model:          openai.SpeechModelGPT4oMiniTTS,
		Voice:          openai.AudioSpeechNewParamsVoiceOnyx,
		Instructions:   openai.String("話し方: 自然で親しみやすく、ゆっくりしすぎず、早すぎない普通の会話のスピードで話してください。\n\n声: 普通の日本人の若い女性の声。感情は穏やかで自然なトーンで、日常会話のように聞こえるようにしてください。\n\nイントネーションと発音: 標準的な日本語のイントネーションを使い、教科書的ではなく自然な言い回しで話してください。"),
		ResponseFormat: openai.AudioSpeechNewParamsResponseFormatOpus,
		Speed:          openai.Float(0.75),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate audio: %w", err)
	}
	return resp.Body, nil
}

func (c *Client) ExplainSentence(sentence string) (string, error) {
	ctx := context.Background()

	systemPrompt := `Объясни японское предложение для ученика уровня N5. Добавь фуригану только к трудным словам (как в учебниках). Переведи на русский, затем кратко разберёшь по частям: слово — значение. Сохраняй простой и понятный стиль. Не добавляй дополнительную разбивку на фразы, объясняй только по словам, как в примерах.`

	examples := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("今日の朝は寒かったですか？"),
		openai.AssistantMessage("今日の朝は寒かったですか？\n→ Сегодня утром было холодно?\n\n今日（きょう） — сегодня\n朝（あさ） — утро\n寒（さむ）かった — было холодно (прош. форма от 寒い)\nですか — вежливая форма вопроса"),
		openai.UserMessage("明日は雨が降ります。"),
		openai.AssistantMessage("明日（あした）は雨（あめ）が降（ふ）ります。\n→ Завтра будет дождь.\n\n明日（あした） — завтра\n雨（あめ） — дождь\n降（ふ）ります — идёт (о дожде/снеге), форма глагола 降る\nが — частица, указывает на подлежащее\nは — тема"),
	}

	userPrompt := openai.UserMessage(sentence)

	messages := append([]openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
	}, examples...)
	messages = append(messages, userPrompt)

	resp, err := c.openaiClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    ChatGPTModel,
		Messages: messages,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfText: &openai.ResponseFormatTextParam{},
		},
	})
	if err != nil {
		return "", fmt.Errorf("GPT request failed: %w", err)
	}

	return resp.Choices[0].Message.Content, nil
}
