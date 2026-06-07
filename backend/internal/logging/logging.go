package logging

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Logger struct {
	scope     string
	requestID string
	method    string
	path      string
}

func ForRequest(scope string, r *http.Request) Logger {
	requestID := strings.TrimSpace(r.Header.Get("x-request-id"))
	if requestID == "" {
		requestID = uuid.NewString()[:8]
	}
	if len(requestID) > 64 {
		requestID = requestID[:64]
	}
	return Logger{scope: scope, requestID: requestID, method: r.Method, path: r.URL.Path}
}

func ForBackground(scope string) Logger {
	return Logger{scope: scope, requestID: uuid.NewString()[:8], method: "BACKGROUND", path: ""}
}

func (l Logger) Debug(message string, meta map[string]any) { l.write("debug", message, meta) }
func (l Logger) Info(message string, meta map[string]any)  { l.write("info", message, meta) }
func (l Logger) Warn(message string, meta map[string]any)  { l.write("warn", message, meta) }
func (l Logger) Error(message string, meta map[string]any) { l.write("error", message, meta) }

func (l Logger) write(level string, message string, meta map[string]any) {
	if !shouldLog(level) {
		return
	}
	fields := map[string]any{"requestId": l.requestID, "method": l.method, "path": l.path}
	for key, value := range meta {
		if value != nil {
			fields[key] = value
		}
	}
	payload, _ := json.Marshal(fields)
	line := fmt.Sprintf("%s %-5s [%s] %s %s", time.Now().UTC().Format(time.RFC3339Nano), strings.ToUpper(level), l.scope, clean(message), string(payload))
	if level == "error" {
		log.New(os.Stderr, "", 0).Println(line)
		return
	}
	log.New(os.Stdout, "", 0).Println(line)
}

func ErrorMeta(err error) map[string]any {
	if err == nil {
		return map[string]any{}
	}
	return map[string]any{"errorName": fmt.Sprintf("%T", err), "errorMessage": err.Error()}
}

func ElapsedMs(start time.Time) int64 {
	return max(0, time.Since(start).Milliseconds())
}

func shouldLog(level string) bool {
	weights := map[string]int{"debug": 10, "info": 20, "warn": 30, "error": 40}
	minLevel := strings.ToLower(strings.TrimSpace(os.Getenv("SERVER_LOG_LEVEL")))
	if minLevel == "" {
		if os.Getenv("NODE_ENV") == "development" {
			minLevel = "debug"
		} else {
			minLevel = "info"
		}
	}
	return weights[level] >= weights[minLevel]
}

func clean(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
