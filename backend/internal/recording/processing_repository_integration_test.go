package recording

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
)

func TestSQLProcessingRepositoryCompletesInterviewAtomically(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	database, err := db.Connect(ctx, databaseURL, false)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	repository := NewSQLProcessingRepository(database)
	t.Run("rewrite uses the current profile level instead of the interview snapshot", func(t *testing.T) {
		fixture := newInterviewCompletionFixture(t, database)
		if _, err := database.Exec(ctx, `UPDATE users SET english_level='a2' WHERE id=$1`, fixture.userID); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(ctx, `UPDATE interview_sessions SET english_level='c1' WHERE id=$1`, fixture.sessionID); err != nil {
			t.Fatal(err)
		}
		work, found, err := repository.LoadProcessingWork(ctx, fixture.job)
		if err != nil || !found || work.EnglishLevel != "a2" {
			t.Fatalf("profile level=%q found=%v err=%v", work.EnglishLevel, found, err)
		}
	})

	t.Run("stores corrected turns, chronological script, and shadowing job", func(t *testing.T) {
		fixture := newInterviewCompletionFixture(t, database)
		corrected := RewriteResult{
			CorrectedTranscript: "Where did you go? I went to Rome. What did you enjoy? I enjoyed the museums.",
			CorrectedAnswers: []CorrectedInterviewAnswer{
				{Sequence: 1, CorrectedAnswerText: "I went to Rome."},
				{Sequence: 2, CorrectedAnswerText: "I enjoyed the museums."},
			},
		}

		completed, err := repository.CompleteRecording(ctx, fixture.job, corrected, fixture.shadowingJobID)
		if err != nil || !completed {
			t.Fatalf("complete recording: completed=%v err=%v", completed, err)
		}

		var status, correctedTranscript, shadowingStatus string
		var shadowingAttemptID sql.NullString
		if err := database.QueryRow(ctx, `
			SELECT status, corrected_transcript, shadowing_status, shadowing_attempt_id
			FROM recordings WHERE id = $1`, fixture.recordingID).Scan(
			&status, &correctedTranscript, &shadowingStatus, &shadowingAttemptID,
		); err != nil {
			t.Fatalf("load completed recording: %v", err)
		}
		if status != "ready" || correctedTranscript != corrected.CorrectedTranscript ||
			shadowingStatus != "processing" || !shadowingAttemptID.Valid ||
			shadowingAttemptID.String != fixture.shadowingJobID {
			t.Fatalf("unexpected completed recording: status=%q corrected=%q shadowing=%q attempt=%q",
				status, correctedTranscript, shadowingStatus, shadowingAttemptID.String)
		}

		rows, err := database.Query(ctx, `
			SELECT corrected_answer_text FROM interview_turns
			WHERE session_id = $1 ORDER BY seq`, fixture.sessionID)
		if err != nil {
			t.Fatalf("load corrected answers: %v", err)
		}
		var answers []string
		for rows.Next() {
			var answer string
			if err := rows.Scan(&answer); err != nil {
				rows.Close()
				t.Fatalf("scan corrected answer: %v", err)
			}
			answers = append(answers, answer)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("iterate corrected answers: %v", err)
		}
		rows.Close()
		if len(answers) != 2 || answers[0] != corrected.CorrectedAnswers[0].CorrectedAnswerText ||
			answers[1] != corrected.CorrectedAnswers[1].CorrectedAnswerText {
			t.Fatalf("corrected answers = %#v", answers)
		}
		saved, found, err := NewSQLQueryRepository(database).Find(ctx, fixture.userID, fixture.recordingID)
		if err != nil || !found {
			t.Fatalf("load completed recording through query repository: found=%v err=%v", found, err)
		}
		if len(saved.InterviewTurns) != 2 ||
			saved.InterviewTurns[0].Question != "Where did you go?" ||
			saved.InterviewTurns[0].CorrectedAnswerText != "I went to Rome." ||
			saved.InterviewTurns[1].Question != "What did you enjoy?" ||
			saved.InterviewTurns[1].CorrectedAnswerText != "I enjoyed the museums." {
			t.Fatalf("saved interview turns = %#v", saved.InterviewTurns)
		}

		var kind, resourceID, idempotencyKey, jobState string
		if err := database.QueryRow(ctx, `
			SELECT kind, resource_id, idempotency_key, state
			FROM processing_jobs WHERE id = $1`, fixture.shadowingJobID).Scan(
			&kind, &resourceID, &idempotencyKey, &jobState,
		); err != nil {
			t.Fatalf("load shadowing job: %v", err)
		}
		if kind != workqueue.KindShadowingSynthesize || resourceID != fixture.recordingID ||
			idempotencyKey != "shadowing:"+fixture.shadowingJobID || jobState != "queued" {
			t.Fatalf("unexpected shadowing job: kind=%q resource=%q key=%q state=%q",
				kind, resourceID, idempotencyKey, jobState)
		}
	})

	invalidResults := []struct {
		name   string
		result RewriteResult
	}{
		{
			name: "mismatched sequence",
			result: RewriteResult{
				CorrectedTranscript: "Where did you go? I went to Rome. What did you enjoy? I enjoyed the museums.",
				CorrectedAnswers: []CorrectedInterviewAnswer{
					{Sequence: 2, CorrectedAnswerText: "I went to Rome."},
					{Sequence: 1, CorrectedAnswerText: "I enjoyed the museums."},
				},
			},
		},
		{
			name: "empty answer",
			result: RewriteResult{
				CorrectedTranscript: "Where did you go? What did you enjoy? I enjoyed the museums.",
				CorrectedAnswers: []CorrectedInterviewAnswer{
					{Sequence: 1, CorrectedAnswerText: ""},
					{Sequence: 2, CorrectedAnswerText: "I enjoyed the museums."},
				},
			},
		},
	}
	for _, test := range invalidResults {
		t.Run(test.name+" rolls back", func(t *testing.T) {
			fixture := newInterviewCompletionFixture(t, database)
			completed, err := repository.CompleteRecording(ctx, fixture.job, test.result, fixture.shadowingJobID)
			if err == nil || completed {
				t.Fatalf("invalid completion: completed=%v err=%v", completed, err)
			}
			assertInterviewCompletionRolledBack(t, database, fixture)
		})
	}
}

type interviewCompletionFixture struct {
	userID         string
	recordingID    string
	sessionID      string
	shadowingJobID string
	job            ProcessingJob
}

func newInterviewCompletionFixture(t *testing.T, database *db.DB) interviewCompletionFixture {
	t.Helper()
	ctx := context.Background()
	userID := uuid.NewString()
	recordingID := uuid.NewString()
	sessionID := uuid.NewString()
	jobID := uuid.NewString()
	leaseToken := uuid.NewString()
	shadowingJobID := uuid.NewString()
	if _, err := database.Exec(ctx, `
		INSERT INTO users (id, email, password_hash)
		VALUES ($1, $2, 'integration-test')`,
		userID, fmt.Sprintf("recording-completion-%s@example.com", userID)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM processing_jobs WHERE resource_id = $1`, recordingID)
		_, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	if _, err := database.Exec(ctx, `
		INSERT INTO processing_jobs
		  (id, kind, resource_id, idempotency_key, state, attempts, max_attempts,
		   lease_token, lease_owner, lease_expires_at, heartbeat_at)
		VALUES ($1, $2, $3, $4, 'running', 1, 3, $5, 'recording-integration-test',
		        NOW() + INTERVAL '5 minutes', NOW())`,
		jobID, workqueue.KindRecordingProcess, recordingID, "recording-test:"+jobID, leaseToken); err != nil {
		t.Fatalf("insert processing job: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, corrected_transcript,
		   suggestions, status, processing_stage, practice_type, processing_job_id,
		   shadowing_status, shadowing_updated_at)
		VALUES ($1, $2, 'Travel', 30, NOW(),
		        'I go to Rome. I enjoy the museums.', '', '[]'::jsonb,
		        'processing', 'rewriting', 'topic', $3, 'pending', NOW())`,
		recordingID, userID, jobID); err != nil {
		t.Fatalf("insert recording: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO interview_sessions
		  (id, owner_principal_id, create_key, request_digest, user_id, topic,
		   opening_question, status, max_duration_seconds, recording_id)
		VALUES ($1, $2, $3, $4, $2, 'Travel', 'Where did you go?',
		        'finalizing', 600, $5)`,
		sessionID, userID, "create-"+uuid.NewString(), uuid.NewString(), recordingID); err != nil {
		t.Fatalf("insert interview session: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO interview_turns
		  (id, session_id, seq, question, asked_at_ms, ended_at_ms,
		   provisional_transcript, final_transcript, transcript_status, transcript_origin)
		VALUES
		  ($1, $3, 1, 'Where did you go?', 0, 10000,
		   'I go to Rome.', 'I go to Rome.', 'ready', 'turn_realtime'),
		  ($2, $3, 2, 'What did you enjoy?', 10000, 20000,
		   'I enjoy the museums.', 'I enjoy the museums.', 'ready', 'turn_realtime')`,
		uuid.NewString(), uuid.NewString(), sessionID); err != nil {
		t.Fatalf("insert interview turns: %v", err)
	}
	return interviewCompletionFixture{
		userID: userID, recordingID: recordingID, sessionID: sessionID, shadowingJobID: shadowingJobID,
		job: ProcessingJob{ID: jobID, ResourceID: recordingID, LeaseToken: leaseToken},
	}
}

func assertInterviewCompletionRolledBack(t *testing.T, database *db.DB, fixture interviewCompletionFixture) {
	t.Helper()
	ctx := context.Background()
	var status, correctedTranscript, shadowingStatus string
	var shadowingAttemptID sql.NullString
	if err := database.QueryRow(ctx, `
		SELECT status, corrected_transcript, shadowing_status, shadowing_attempt_id
		FROM recordings WHERE id = $1`, fixture.recordingID).Scan(
		&status, &correctedTranscript, &shadowingStatus, &shadowingAttemptID,
	); err != nil {
		t.Fatalf("load rolled-back recording: %v", err)
	}
	if status != "processing" || correctedTranscript != "" || shadowingStatus != "pending" || shadowingAttemptID.Valid {
		t.Fatalf("recording was partially completed: status=%q corrected=%q shadowing=%q attempt=%q",
			status, correctedTranscript, shadowingStatus, shadowingAttemptID.String)
	}
	var correctedCount int
	if err := database.QueryRow(ctx, `
		SELECT COUNT(*) FROM interview_turns
		WHERE session_id = $1 AND corrected_answer_text IS NOT NULL`, fixture.sessionID).Scan(&correctedCount); err != nil {
		t.Fatalf("count rolled-back corrected answers: %v", err)
	}
	if correctedCount != 0 {
		t.Fatalf("corrected answers survived rollback: %d", correctedCount)
	}
	var shadowingJobs int
	if err := database.QueryRow(ctx, `
		SELECT COUNT(*) FROM processing_jobs WHERE id = $1`, fixture.shadowingJobID).Scan(&shadowingJobs); err != nil {
		t.Fatalf("count rolled-back shadowing jobs: %v", err)
	}
	if shadowingJobs != 0 {
		t.Fatalf("shadowing job survived rollback: %d", shadowingJobs)
	}
}
