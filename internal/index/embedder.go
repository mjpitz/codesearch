package index

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// Embedder generates float32 vector embeddings from text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// OpenAIEmbedder calls an OpenAI-compatible /v1/embeddings endpoint.
type OpenAIEmbedder struct {
	client openai.Client
	model  string
}

// NewEmbedder reads CODESEARCH_EMBED_MODEL from the environment (and optionally
// CODESEARCH_EMBED_URL and CODESEARCH_EMBED_API_KEY) and constructs an embedder.
// Returns nil, nil when CODESEARCH_EMBED_MODEL is unset — callers should treat
// nil as "no embedder configured".
func NewEmbedder() (*OpenAIEmbedder, error) {
	model := os.Getenv("CODESEARCH_EMBED_MODEL")
	if model == "" {
		return nil, nil
	}

	var opts []option.RequestOption

	if url := os.Getenv("CODESEARCH_EMBED_URL"); url != "" {
		opts = append(opts, option.WithBaseURL(url))
	}

	if key := os.Getenv("CODESEARCH_EMBED_API_KEY"); key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}

	client := openai.NewClient(opts...)

	return &OpenAIEmbedder{client: client, model: model}, nil
}

// Embed returns a float32 embedding vector for the given text.
func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input:          openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
		Model:          openai.EmbeddingModel(e.model),
		EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat,
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embed: empty response from server")
	}

	f64 := resp.Data[0].Embedding
	vec := make([]float32, len(f64))

	for i, v := range f64 {
		vec[i] = float32(v)
	}

	return vec, nil
}

// Float32ToBytes encodes a float32 slice as little-endian bytes.
func Float32ToBytes(vec []float32) []byte {
	b := make([]byte, len(vec)*4)

	for i, v := range vec {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}

	return b
}

// BytesToFloat32 decodes little-endian bytes to a float32 slice.
// Returns nil if b length is not a multiple of 4.
func BytesToFloat32(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}

	vec := make([]float32, len(b)/4)

	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}

	return vec
}
