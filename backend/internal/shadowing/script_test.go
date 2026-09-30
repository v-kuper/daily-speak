package shadowing

import "testing"

func TestLegacySampleRemainsReadableForReplacement(t *testing.T) {
	payload := []byte(`{"englishLevel":"b1","text":"Where did you go? I went to a park.","turns":[{"sequence":1,"question":"Where did you go?","answerText":"I went to a park."}]}`)
	script := DecodeScript(payload)
	if script == nil || script.Turns[0].AnswerText != "I went to a park." {
		t.Fatal("legacy sample cannot be identified for replacement")
	}
	if DecodeScript([]byte(`{"englishLevel":"a2","text":"stale","turns":[{"sequence":1,"question":"What?","answerText":"I work."}]}`)) != nil {
		t.Fatal("mismatched legacy script accepted")
	}
}
