package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
)

type rawEvent struct {
	Properties properties `json:"properties"`
	Context    eventCtx   `json:"context"`
}

type properties struct {
	RequestID    string  `json:"request_id"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Status       string  `json:"status"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	LatencyMs    int     `json:"latency_ms"`
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
	Stream       bool    `json:"stream"`
	FinishReason string  `json:"finish_reason"`
	Origin       string  `json:"origin"`
}

type eventCtx struct {
	EventContractID string `json:"event_contract_id"`
	Event           string `json:"event"`
	AppName         string `json:"app_name"`
	AppVersion      string `json:"app_version"`
	CreatedAt       string `json:"created_at"`
	UserID          string `json:"user_id"`
	OrgID           string `json:"org_id"`
	Environment     string `json:"environment"`
}

var (
	providers = []string{"openai", "anthropic", "google", "github_copilot"}
	models    = map[string][]string{
		"openai":         {"gpt-4o", "gpt-4o-mini", "o3", "o4-mini", "gpt-4.1"},
		"anthropic":      {"claude-opus-4", "claude-sonnet-4", "claude-3.5-haiku", "claude-3-opus"},
		"google":         {"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0-flash", "gemma-3"},
		"github_copilot": {"copilot-gpt-4o", "copilot-claude-sonnet", "copilot-gemini-flash"},
	}
	statuses      = []string{"success", "error", "rate_limited", "timeout", "content_filtered"}
	finishReasons = []string{"stop", "length", "content_filter", "tool_calls", "error"}
	eventNames    = []string{
		"completion_request", "chat_completion", "embedding_request",
		"function_call", "streaming_response", "model_evaluation",
	}
	origins      = []string{"api", "playground", "sdk", "copilot_ide", "chatbot", "batch"}
	appNames     = []string{"quanta-ai-gateway", "inference-router", "prompt-hub", "eval-pipeline"}
	appVersions  = []string{"2.4.0", "2.3.1", "3.0.0-beta", "2.2.5", "1.9.8"}
	environments = []string{"production", "staging", "canary", "development"}
	orgs         = []string{"org-acme", "org-globex", "org-initech", "org-umbrella", "org-wayne", "org-stark"}
)

func main() {
	var (
		brokers = flag.String("brokers", env("SEED_BROKERS", "localhost:9094"), "Kafka brokers (comma-separated)")
		topic   = flag.String("topic", env("SEED_TOPIC", "event-tracking_track-events-approved"), "target topic")
		count   = flag.Int("count", 100, "total number of events to produce")
		delay   = flag.Duration("delay", 0, "delay between batches (0 = no delay)")
	)
	flag.Parse()

	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	cfg.Producer.Return.Errors = true
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Flush.Messages = 500
	cfg.Producer.Flush.Frequency = 10 * time.Millisecond

	brokerList := strings.Split(*brokers, ",")
	producer, err := sarama.NewAsyncProducer(brokerList, cfg)
	if err != nil {
		log.Fatalf("producer: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var acked atomic.Int64
	var errCount atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case _, ok := <-producer.Successes():
				if !ok {
					return
				}
				acked.Add(1)
			case e, ok := <-producer.Errors():
				if !ok {
					return
				}
				errCount.Add(1)
				log.Printf("produce error: %v", e.Err)
			}
		}
	}()

	log.Printf("seeding %d events to %s via %s", *count, *topic, *brokers)
	sent := 0
	for sent < *count {
		select {
		case <-ctx.Done():
			log.Printf("interrupted after %d enqueued", sent)
			goto shutdown
		default:
		}

		ev := generate(sent)
		payload, err := json.Marshal(ev)
		if err != nil {
			log.Fatalf("marshal: %v", err)
		}

		producer.Input() <- &sarama.ProducerMessage{
			Topic: *topic,
			Key:   sarama.StringEncoder(ev.Context.EventContractID),
			Value: sarama.ByteEncoder(payload),
		}
		sent++

		if sent%10000 == 0 {
			log.Printf("enqueued %d/%d (acked %d)", sent, *count, acked.Load())
		}

		if *delay > 0 && sent%500 == 0 {
			time.Sleep(*delay)
		}
	}

shutdown:
	producer.AsyncClose()
	<-done
	log.Printf("done — enqueued=%d acked=%d errors=%d to %s", sent, acked.Load(), errCount.Load(), *topic)
}

func generate(seq int) rawEvent {
	now := time.Now().UTC()
	provider := pick(providers)
	model := pick(models[provider])

	return rawEvent{
		Properties: properties{
			RequestID:    fmt.Sprintf("req-%06d-%s", seq, hex.EncodeToString(randomBytes(4))),
			Provider:     provider,
			Model:        model,
			Status:       pick(statuses),
			InputTokens:  rand.IntN(4000) + 50,
			OutputTokens: rand.IntN(2000) + 10,
			LatencyMs:    rand.IntN(5000) + 100,
			Temperature:  float64(rand.IntN(20)) / 20.0,
			MaxTokens:    (rand.IntN(8) + 1) * 512,
			Stream:       rand.IntN(3) == 0,
			FinishReason: pick(finishReasons),
			Origin:       pick(origins),
		},
		Context: eventCtx{
			EventContractID: fmt.Sprintf("evt-%06d-%s", seq, hex.EncodeToString(randomBytes(3))),
			Event:           pick(eventNames),
			AppName:         pick(appNames),
			AppVersion:      pick(appVersions),
			CreatedAt:       now.Add(-time.Duration(rand.IntN(3600)) * time.Second).Format(time.RFC3339),
			UserID:          fmt.Sprintf("usr-%06d", rand.IntN(50000)),
			OrgID:           pick(orgs),
			Environment:     pick(environments),
		},
	}
}

func pick(s []string) string { return s[rand.IntN(len(s))] }

func randomBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rand.IntN(256))
	}
	return b
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
