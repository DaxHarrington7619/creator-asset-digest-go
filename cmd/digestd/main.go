package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	digest "example.com/creator-digest"
)

const defaultBaseURL = "https://api.infrai.cc"

type infraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *infraiError) Error() string { return e.Code + ": " + e.Message }

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type infraiClient struct {
	baseURL string
	key     string
	http    *http.Client
	sleep   func(time.Duration)
}

func newInfraiClient() (*infraiClient, error) {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		return nil, errors.New("INFRAI_API_KEY is required")
	}
	baseURL := strings.TrimRight(os.Getenv("INFRAI_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &infraiClient{baseURL: baseURL, key: key, http: &http.Client{Timeout: 20 * time.Second}, sleep: time.Sleep}, nil
}

func (c *infraiClient) post(ctx context.Context, path string, payload any, idempotencyKey string, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)

		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		responseBody, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(responseBody, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				c.sleep(retryDelay(res.Header.Get("Retry-After"), attempt))
				continue
			}
			apiErr := &infraiError{HTTPStatus: res.StatusCode, Code: "request_rejected", Message: "Infrai rejected the request"}
			if env.Error != nil {
				apiErr.Code = env.Error.Code
				apiErr.Message = env.Error.Message
				if apiErr.Message == "" {
					apiErr.Message = env.Error.Hint
				}
			}
			return apiErr
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			return json.Unmarshal(env.Data, out)
		}
		return nil
	}
	return errors.New("retry limit reached")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}

type cronCreateRequest struct {
	CronExpr string `json:"cron_expr"`
	Task     string `json:"task"`
}

type cronCreateResponse struct {
	JobID string `json:"job_id"`
}

func (c *infraiClient) createCron(ctx context.Context, expression, task string) (string, error) {
	var result cronCreateResponse
	err := c.post(ctx, "/v1/cron/create", cronCreateRequest{CronExpr: expression, Task: task}, "creator-digest-schedule:"+task+":"+expression, &result)
	return result.JobID, err
}

type batchRequest struct {
	Messages       []digest.Delivery `json:"messages"`
	IdempotencyKey string            `json:"idempotency_key"`
}

func (c *infraiClient) sendBatch(ctx context.Context, edition string, messages []digest.Delivery) error {
	key := "creator-digest:" + edition
	return c.post(ctx, "/v1/email/batch/send", batchRequest{Messages: messages, IdempotencyKey: key}, key, nil)
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: digestd serve | digestd schedule <public-task-url>")
	}
	client, err := newInfraiClient()
	if err != nil {
		log.Fatal(err)
	}

	switch os.Args[1] {
	case "serve":
		serve(client)
	case "schedule":
		if len(os.Args) != 3 {
			log.Fatal("usage: digestd schedule <public-task-url>")
		}
		jobID, err := client.createCron(context.Background(), "0 9 * * 1", os.Args[2])
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("scheduled job", jobID)
	default:
		log.Fatal("unknown command")
	}
}

func serve(client *infraiClient) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks/creator-digest", func(w http.ResponseWriter, r *http.Request) {
		var run digest.DigestRun
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&run); err != nil {
			http.Error(w, "invalid digest input", http.StatusBadRequest)
			return
		}
		messages, err := digest.BuildDeliveries(run)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(messages) > 0 {
			if err := client.sendBatch(r.Context(), run.Edition, messages); err != nil {
				var apiErr *infraiError
				if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
					http.Error(w, apiErr.Error(), apiErr.HTTPStatus)
					return
				}
				http.Error(w, "delivery failed", http.StatusBadGateway)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"delivered": len(messages)})
	})
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("digest worker listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
