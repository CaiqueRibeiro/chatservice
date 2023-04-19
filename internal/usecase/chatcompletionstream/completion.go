package chatcompletionstream

import (
	openai "github.com/sashabaranov/go-openai"
	"github.com/CaiqueRibeiro/chatservice/internal/domain/gateway"

)

type ChatCompletionConfigInputDTO struct {
	Model string
	ModelMaxTokens 				int
	Temperature 					float32
	TopP									float32
	N											int
	Stop									[]string
	MaxTokens 						int
	PresencePenalty 			float32
	FrequencyPenalty			float32
	InitialSystemMessage	string
}

type ChatCompletionInputDTO struct {
	ChatID      string                       `json:"chat_id,omitempty"`
	UserID      string                       `json:"user_id"`
	UserMessage string                       `json:"user_message"`
	Config      ChatCompletionConfigInputDTO `json:"config"`
}

type ChatCompletionOutputDTO struct {
	ChatID  string `json:"chat_id"`
	UserID  string `json:"user_id"`
	Content string `json:"content"`
}

type ChatCompletionUseCase struct {
	ChatGateway 	gateway.ChatGateway
	OpenAiClient *openai.Client
}

func NewChatCompletionUseCase(chatGateway gateway.ChatGateway, openAiClient *openai.Client) *ChatCompletionUseCase {
	return &ChatCompletionUseCase{
		ChatGateway: chatGateway,
		OpenAiClient: openAiClient
	}
}

func (uc *ChatCompletionUseCase) Execute(ctx context.Context, input ChatCompletionInputDTO) (*ChatCompletionOutputDTO, error) {
	chat, err := uc.ChatGateway.FindChatByID(ctx, input.ChatID)
	if err != nil {
		if err.Error() == "chat not found" {
			chat, err = createNewChat(input)
			if err != nil {
				return nil, errors.New("error creating new chat: " + err.Error())
			}
			err = uc.ChatGateway.CreateChat(ctx, chat)
			if err != nil {
				return nil, errors.New("error persisting new chat: " + err.Error())
			}
		} else {
			return nil, errors.New("error fetching existing chat: " + err.Error())
		}
	}
	userMessage, err := entity.NewMessage("user", input.UserMessage, chat.Config.Model)
	if err != nil {
		return nil, errors.New("error creating new message: " + err.Error())
	}
	err = chat.AddMessage(userMessage)
	if err != nil {
		return nil, errors.New("error adding new message: " + err.Error())
	}
	messages := []openai.ChatCompletionMessage{}
	for _, msg := range chat.Messages {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	resp, err := uc.OpenAIClient.CreateChatCompletionStream(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:            chat.Config.Model.Name,
			Messages:         messages,
			MaxTokens:        chat.Config.MaxTokens,
			Temperature:      chat.Config.Temperature,
			TopP:             chat.Config.TopP,
			PresencePenalty:  chat.Config.PresencePenalty,
			FrequencyPenalty: chat.Config.FrequencyPenalty,
			Stop:             chat.Config.Stop,
			Stream:           true,
		},
	)
	if err != nil {
		return nil, errors.New("error openai: " + err.Error())
	}
	var fullResponse strings.Builder
	for {
		response, err := resp.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("error streaming response: " + err.Error())
		}
		fullResponse.WriteString(response.Choices[0].Delta.Content)
		r := ChatCompletionOutputDTO{
			ChatID:  chat.ID,
			UserID:  input.UserID,
			Content: fullResponse.String(),
		}
		uc.Stream <- r
	}
	assistant, err := entity.NewMessage("assistant", fullResponse.String(), chat.Config.Model)
	if err != nil {
		return nil, err
	}
	err = chat.AddMessage(assistant)
	if err != nil {
		return nil, err
	}
	err = uc.ChatGateway.SaveChat(ctx, chat)
	if err != nil {
		return nil, err
	}
	output := &ChatCompletionOutputDTO{
		ChatID:  chat.ID,
		UserID:  input.UserID,
		Content: fullResponse.String(),
	}
	return output, nil
}

func createNewChat(input ChatCompletionInputDTO) (*entity.Chat, error) {
	model := entity.NewModel(input.Config.Model, input.Config.ModelMaxTokens)
	chatConfig := &entity.ChatConfig{
		Temperature:      input.Config.Temperature,
		TopP:             input.Config.TopP,
		N:                input.Config.N,
		Stop:             input.Config.Stop,
		MaxTokens:        input.Config.MaxTokens,
		PresencePenalty:  input.Config.PresencePenalty,
		FrequencyPenalty: input.Config.FrequencyPenalty,
		Model:            model,
	}

	initialMessage, err := entity.NewMessage("system", input.Config.InitialSystemMessage, model)
	if err != nil {
		return nil, errors.New("error creating initial message: " + err.Error())
	}
	chat, err := entity.NewChat(input.UserID, initialMessage, chatConfig)
	if err != nil {
		return nil, errors.New("error creating new chat: " + err.Error())
	}
	return chat, nil
}