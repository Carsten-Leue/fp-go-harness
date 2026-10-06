package replay

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	M "github.com/IBM/fp-go/v2/monoid"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactText(t *testing.T) {
	redact := RedactText()

	tests := []struct {
		name, in, want string
	}{
		{"bearer", `Authorization: Bearer abc.def-123`, `Authorization: Bearer <redacted>`},
		{"jwt", `token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln end`, `token <redacted> end`},
		{"embedded json token", `{"refresh_token":"r-123","x":1}`, `{"refresh_token":"<redacted>","x":1}`},
		{"embedded json cookie and api key", `{"cookie":"sid=abc","x-api-key":"k-1"}`, `{"cookie":"<redacted>","x-api-key":"<redacted>"}`},
		{"email", `mail jane.doe@corp.example.org now`, `mail user@example.com now`},
		{"ibm host keeps path", `GET https://api.us-east.bob.ibm.com/admin/v1/profile`, `GET https://redacted.example.com/admin/v1/profile`},
		{"instana", `https://prod-x.instana.io/api`, `https://redacted.example.com/api`},
		{"private ip", `http://192.168.178.201:8080/app`, `http://192.0.2.1:8080/app`},
		{"windows home", `C:\Users\JaneDoe\.bob\logs`, `C:\Users\user\.bob\logs`},
		{"windows url home", `file:///c:/Users/JaneDoe/AppData`, `file:///c:/Users/user/AppData`},
		{"unix home", `/home/jane/src`, `/home/user/src`},
		{"public host unchanged", `https://json-schema.org/draft`, `https://json-schema.org/draft`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, redact(tt.in))
		})
	}
}

func TestRedactTerms(t *testing.T) {
	redact := RedactTerms("jane-doe", "Jane")

	assert.Equal(t, "user and user, user", redact("Jane-Doe and jane-doe, JANE"))
}

func TestRedact(t *testing.T) {
	rec := Record{
		Module: "Gateway",
		Msg:    "HTTP request for jane@corp.example.org",
		Data: json.RawMessage(`{"url":"https://api.dev.bob.ibm.com/v1/chat/completions?x=<a>","headers":{"Authorization":"Bearer secret","Accept":"*/*"},` +
			`"messages":[{"content":"Bearer secret\ncarsten@corp.example.org"}],"n":12345678901234567890,"ok":true,"none":null}`),
	}

	got := Redact(RedactText())(rec)

	assert.Equal(t, result.Of(Record{
		Module: "Gateway",
		Msg:    "HTTP request for user@example.com",
		Data: json.RawMessage(`{"headers":{"Accept":"*/*","Authorization":"<redacted>"},` +
			`"messages":[{"content":"Bearer <redacted>\nuser@example.com"}],"n":12345678901234567890,"none":null,"ok":true,` +
			`"url":"https://redacted.example.com/v1/chat/completions?x=<a>"}`),
	}), got)
}

func TestRedactSensitiveNonString(t *testing.T) {
	rec := Record{Data: json.RawMessage(`{"headers":{"set-cookie":["sid=abc; Path=/"]},"password":12345,"token":null,"cookie":null}`)}

	assert.Equal(t, result.Of(Record{
		Data: json.RawMessage(`{"cookie":null,"headers":{"set-cookie":"<redacted>"},"password":"<redacted>","token":null}`),
	}), Redact(RedactText())(rec))
}

func TestRedactStringData(t *testing.T) {
	rec := Record{Module: "Auth", Msg: "Auth state changed to", Data: json.RawMessage(`"expired"`)}

	assert.Equal(t, result.Of(rec), Redact(RedactText())(rec))
}

func TestRedactAuthObject(t *testing.T) {
	rec := Record{
		Module: "Auth",
		Msg:    "Token refresh failed",
		Data:   json.RawMessage(`{"message":"invalid refresh token","stack":"at c:/Users/JaneDoe/x.js"}`),
	}

	assert.Equal(t, result.Of(Record{
		Module: "Auth",
		Msg:    "Token refresh failed",
		Data:   json.RawMessage(`"<redacted>"`),
	}), Redact(RedactText())(rec))
}

func TestRedactNoData(t *testing.T) {
	rec := Record{Module: "Extension", Msg: "Activating extension"}

	assert.Equal(t, result.Of(rec), Redact(RedactText())(rec))
}

func TestRedactInvalidData(t *testing.T) {
	rec := Record{Data: json.RawMessage(`{`)}

	assert.True(t, result.IsLeft(Redact(RedactText())(rec)))
}

// envRedactTerms holds the names of the people in the recordings, comma
// separated, longer terms first. RedactText can't know them, and they stay out
// of the repository.
const envRedactTerms = "REPLAY_REDACT_TERMS"

func redactTerms(t *testing.T) []string {
	return F.Pipe4(
		lookupTestEnv(t, envRedactTerms),
		result.GetOrElse(F.Constant1[error]("")),
		F.Bind2nd(strings.Split, ","),
		A.Map(strings.TrimSpace),
		A.Filter(S.IsNonEmpty),
	)
}

// genericLeaks are the patterns RedactText removes.
var genericLeaks = []string{
	`ibm\.com|ibm\.biz|instana\.io`,
	`[a-z0-9._%+-]+@(?:[a-z0-9-]+\.)+(?:com|net|org|de)\b`,
	`192\.168\.\d`,
	`\beyJ[a-z0-9_-]{10,}`,
	`bearer\s+(?:[a-z0-9._~+/=-]+)`,
}

// leakPatterns must not match anything in a redacted recording.
func leakPatterns(terms []string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + F.Pipe2(
		genericLeaks,
		A.Concat(A.Map(regexp.QuoteMeta)(terms)),
		M.ConcatAll(S.IntersperseMonoid("|")),
	))
}

// placeholders are removed before the leak check, because leakPatterns matches
// RedactedEmail itself.
var placeholders = regexp.MustCompile(regexp.QuoteMeta(RedactedEmail))

func TestRedactRecordings(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(recordingsDir(t), "*.log"))
	require.NoError(t, err)
	if len(files) == 0 {
		t.Skip("recordings not available")
	}

	terms := redactTerms(t)
	leaks := leakPatterns(terms)
	redact := Redact(F.Flow2(
		RedactText(),
		RedactTerms(terms...),
	))

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			records, err := result.Unwrap(iterresult.Collect(iterresult.ChainEitherK(redact)(ReadLogFile()(file)))())
			require.NoError(t, err)

			for i, rec := range records {
				line, err := json.Marshal(rec)
				require.NoError(t, err)
				for _, leak := range leaks.FindAllString(placeholders.ReplaceAllString(string(line), ""), -1) {
					t.Errorf("record %d (%s): %q survived redaction", i, rec.Module, leak)
				}
			}
		})
	}
}
