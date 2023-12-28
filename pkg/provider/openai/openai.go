package openai

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/adrianliechti/llama/pkg/provider"
	"github.com/sashabaranov/go-openai"
)

var (
	_ provider.Provider = &Provider{}
)

var (
	ErrInvalidModelMapping = errors.New("invalid model mapping")
)

type Provider struct {
	url   string
	token string

	client *openai.Client
	mapper ModelMapper
}

type ModelMapper interface {
	From(key string) string
	To(key string) string
}

type Option func(*Provider)

func New(options ...Option) *Provider {
	p := &Provider{}

	for _, option := range options {
		option(p)
	}

	config := openai.DefaultConfig(p.token)

	if p.url != "" {
		config.BaseURL = p.url
	}

	if strings.Contains(p.url, "openai.azure.com") {
		config = openai.DefaultAzureConfig(p.token, p.url)
	}

	p.client = openai.NewClientWithConfig(config)

	return p
}

func WithURL(url string) Option {
	return func(p *Provider) {
		p.url = url
	}
}

func WithToken(token string) Option {
	return func(p *Provider) {
		p.token = token
	}
}

func WithModelMapper(mapper ModelMapper) Option {
	return func(p *Provider) {
		p.mapper = mapper
	}
}

func (p *Provider) Models(ctx context.Context) ([]provider.Model, error) {
	list, err := p.client.ListModels(ctx)

	if err != nil {
		return nil, err
	}

	var result []provider.Model

	for _, m := range list.Models {
		model := provider.Model{
			ID: m.ID,
		}

		if p.mapper != nil {
			model.ID = p.mapper.From(m.ID)
		}

		if model.ID == "" {
			continue
		}

		result = append(result, model)
	}

	return result, nil
}

func (p *Provider) Embed(ctx context.Context, model, content string) ([]float32, error) {
	// if p.mapper != nil {
	// 	model = p.mapper.To(model)
	// }

	req := openai.EmbeddingRequest{
		Input: content,
		Model: openai.AdaEmbeddingV2,
	}

	result, err := p.client.CreateEmbeddings(ctx, req)

	if err != nil {
		return nil, err
	}

	return result.Data[0].Embedding, nil
}

func (p *Provider) Complete(ctx context.Context, model string, messages []provider.Message, options *provider.CompleteOptions) (*provider.Message, error) {
	if options == nil {
		options = &provider.CompleteOptions{}
	}

	if p.mapper != nil {
		model = p.mapper.To(model)
	}

	if model == "" {
		return nil, ErrInvalidModelMapping
	}

	req, err := p.convertCompletionRequest(model, messages, options)

	if err != nil {
		return nil, err
	}

	completion, err := p.client.CreateChatCompletion(ctx, *req)

	if err != nil {
		return nil, err
	}

	message := provider.Message{
		Role:    toMessageRole(completion.Choices[0].Message.Role),
		Content: completion.Choices[0].Message.Content,
	}

	return &message, nil
}

func (p *Provider) CompleteStream(ctx context.Context, model string, messages []provider.Message, stream chan<- provider.Message, options *provider.CompleteOptions) error {
	if options == nil {
		options = &provider.CompleteOptions{}
	}

	if p.mapper != nil {
		model = p.mapper.To(model)
	}

	if model == "" {
		return ErrInvalidModelMapping
	}

	req, err := p.convertCompletionRequest(model, messages, options)

	if err != nil {
		return err
	}

	result, err := p.client.CreateChatCompletionStream(ctx, *req)

	if err != nil {
		return err
	}

	defer result.Close()

	for {
		completion, err := result.Recv()

		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

		stream <- provider.Message{
			Role:    toMessageRole(completion.Choices[0].Delta.Role),
			Content: completion.Choices[0].Delta.Content,
		}

		if completion.Choices[0].FinishReason != "" {
			break
		}
	}

	return nil
}

func (p *Provider) convertCompletionRequest(model string, messages []provider.Message, options *provider.CompleteOptions) (*openai.ChatCompletionRequest, error) {
	if options == nil {
		options = &provider.CompleteOptions{}
	}

	req := &openai.ChatCompletionRequest{
		Model: model,
	}

	for _, m := range messages {
		req.Messages = append(req.Messages, openai.ChatCompletionMessage{
			Role:    convertMessageRole(m.Role),
			Content: m.Content,
		})
	}

	return req, nil
}

func convertMessageRole(r provider.MessageRole) string {
	switch r {
	case provider.MessageRoleSystem:
		return openai.ChatMessageRoleSystem

	case provider.MessageRoleUser:
		return openai.ChatMessageRoleUser

	case provider.MessageRoleAssistant:
		return openai.ChatMessageRoleAssistant

	default:
		return ""
	}
}

func toMessageRole(r string) provider.MessageRole {
	switch r {
	case openai.ChatMessageRoleSystem:
		return provider.MessageRoleSystem

	case openai.ChatMessageRoleUser:
		return provider.MessageRoleUser

	case openai.ChatMessageRoleAssistant:
		return provider.MessageRoleAssistant

	// case openai.ChatMessageRoleFunction:
	// 	return provider.MessageRoleFunction

	// case openai.ChatMessageRoleTool:
	// 	return provider.MessageRoleTool

	default:
		return provider.MessageRoleAssistant
	}
}
