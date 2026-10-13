package replay

import (
	"bytes"
	"encoding/json"
	"regexp"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/endomorphism"
	F "github.com/IBM/fp-go/v2/function"
	M "github.com/IBM/fp-go/v2/monoid"
	"github.com/IBM/fp-go/v2/optics/lens"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
)

// Placeholders that replace redacted values.
const (
	RedactedValue = "<redacted>"
	RedactedEmail = "user@example.com"
	RedactedHost  = "redacted.example.com"
	RedactedUser  = "user"
	// RedactedIP is from TEST-NET-1 (RFC 5737), reserved for documentation.
	RedactedIP = "192.0.2.1"
)

const authModule = "Auth"

// sensitiveKeys is the regexp alternation of the keys whose values are
// redacted, both in decoded payloads and in JSON embedded in text.
const sensitiveKeys = `access_?token|refresh_?token|id_?token|api_?key|apikey|x-api-key|client_?secret|password|authorization|cookie|set-cookie`

var (
	sensitiveKey = regexp.MustCompile(`(?i)^(?:` + sensitiveKeys + `)$`)

	redactedJSON = json.RawMessage(`"` + RedactedValue + `"`)
)

// replaceAll is a leaf: a constructor over the pattern and its replacement.
func replaceAll(pattern, repl string) Endomorphism[string] {
	return F.Bind2nd(regexp.MustCompile(pattern).ReplaceAllString, repl)
}

// concatAll composes rules so that they run in slice order.
func concatAll() reader.Reader[[]Endomorphism[string], Endomorphism[string]] {
	return M.ConcatAll(M.Reverse(endomorphism.Monoid[string]()))
}

// RedactText replaces the sensitive values found in the recorded logs with
// placeholders: bearer tokens and JWTs, token, key and password fields in
// embedded JSON, e-mail addresses, internal host names (ibm.com, ibm.biz,
// instana.io), private IPv4 addresses and the user name in home directory
// paths. URL paths are kept, so requests can still be matched by path.
func RedactText() Endomorphism[string] {
	return concatAll()(A.From(
		replaceAll(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`, "Bearer "+RedactedValue),
		replaceAll(`\beyJ[A-Za-z0-9_-]{10,}(?:\.[A-Za-z0-9_-]+){0,2}`, RedactedValue),
		replaceAll(`(?i)("(?:`+sensitiveKeys+`)"\s*:\s*")[^"]*"`, `${1}`+RedactedValue+`"`),
		replaceAll(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`, RedactedEmail),
		replaceAll(`(?i)\b(?:[a-z0-9-]+\.)*(?:ibm\.com|ibm\.biz|instana\.io)\b`, RedactedHost),
		replaceAll(`\b(?:10(?:\.\d{1,3}){3}|192\.168(?:\.\d{1,3}){2}|172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2})\b`, RedactedIP),
		replaceAll(`(?i)\b([a-z]:[\\/]+users[\\/]+)[^\\/"\s]+`, `${1}`+RedactedUser),
		replaceAll(`(/(?:home|Users)/)[^/"\s]+`, `${1}`+RedactedUser),
	))
}

// redactTerm replaces one literal term, ignoring case, with [RedactedUser].
func redactTerm() reader.Reader[string, Endomorphism[string]] {
	return F.Flow3(
		regexp.QuoteMeta,
		S.Prepend(`(?i)`),
		F.Bind2nd(replaceAll, RedactedUser),
	)
}

// termRules replaces each literal term, ignoring case, with [RedactedUser],
// in slice order.
func termRules() reader.Reader[[]string, Endomorphism[string]] {
	return F.Flow2(
		A.Map(redactTerm()),
		concatAll(),
	)
}

// RedactTerms replaces each literal term, ignoring case, with [RedactedUser].
// Use it for names and handles that the generic rules of [RedactText] can't
// recognise. Longer terms should come first, as the terms are applied in order.
//
// It is wiring: the variadic form of the term rules, for callers that list the
// terms.
func RedactTerms(terms ...string) Endomorphism[string] {
	return termRules()(terms)
}

// redactValue applies text to every string in a decoded JSON value and replaces
// the non-null values of sensitive keys such as "authorization" with
// [RedactedValue], whatever their type: set-cookie is often an array, a password
// can be a number.
//
// It is a leaf, written as a recursive type switch: the shape of decoded JSON is
// only known at run time, and the nesting depth is bounded by the log line.
func redactValue(text Endomorphism[string]) Endomorphism[any] {
	var walk Endomorphism[any]
	field := func(key string, v any) any {
		if v != nil && sensitiveKey.MatchString(key) {
			return RedactedValue
		}
		return walk(v)
	}
	walk = func(v any) any {
		switch t := v.(type) {
		case string:
			return text(t)
		case []any:
			return A.Map(walk)(t)
		case map[string]any:
			return R.MapWithIndex(field)(t)
		}
		return v
	}
	return walk
}

// decodeJSON is a leaf: the decoder is configured through its methods. It keeps
// numbers as [json.Number] so that they are re-encoded unchanged.
func decodeJSON(m json.RawMessage) Result[any] {
	var v any
	dec := json.NewDecoder(bytes.NewReader(m))
	dec.UseNumber()
	err := dec.Decode(&v)
	return result.TryCatchError(v, err)
}

// encodeJSON is a leaf: the encoder is configured through its methods. It
// doesn't escape <, > and &, so that placeholders stay readable.
func encodeJSON(v any) Result[json.RawMessage] {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	return result.TryCatchError(json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")), err)
}

// isEmptyJSON is a leaf: the predicate of a missing payload.
func isEmptyJSON(m json.RawMessage) bool {
	return len(m) == 0
}

// isJSONObject is a leaf: the predicate of a payload that is a JSON object.
func isJSONObject(m json.RawMessage) bool {
	return bytes.HasPrefix(bytes.TrimSpace(m), []byte("{"))
}

// decodedRedaction redacts the strings inside a JSON payload. The payload is
// decoded first, so the rules see the real text and can't break JSON escapes;
// object keys come out sorted.
func decodedRedaction() reader.Reader[Endomorphism[string], result.Kleisli[json.RawMessage, json.RawMessage]] {
	return F.Flow4(
		redactValue,
		result.Map[any, any],
		reader.Map[Result[any]](result.Chain(encodeJSON)),
		reader.Local[Result[json.RawMessage]](decodeJSON),
	)
}

// skipEmptyJSON keeps a missing payload as it is and hands every other one to
// the redaction.
func skipEmptyJSON() Endomorphism[result.Kleisli[json.RawMessage, json.RawMessage]] {
	return F.Flow2(
		F.Bind2nd(P.Fold[json.RawMessage, Result[json.RawMessage]], result.Of[json.RawMessage]),
		reader.Read[result.Kleisli[json.RawMessage, json.RawMessage]](isEmptyJSON),
	)
}

// redactData redacts the strings inside a JSON payload, see
// [decodedRedaction]. A missing payload stays missing.
func redactData() reader.Reader[Endomorphism[string], result.Kleisli[json.RawMessage, json.RawMessage]] {
	return F.Flow2(decodedRedaction(), skipEmptyJSON())
}

// redactAuthData replaces object payloads of the Auth module (token refresh
// errors and their stack traces) as a whole.
func redactAuthData() Endomorphism[Record] {
	moduleLens := MakeRecordModuleLens()
	dataLens := MakeRecordDataLens()

	isAuthObject := F.Pipe1(
		F.Flow2(moduleLens.Get, P.IsStrictEqual[string]()(authModule)),
		P.And(F.Flow2(dataLens.Get, isJSONObject)),
	)

	return P.Fold(
		F.Identity[Record],
		dataLens.Set(redactedJSON),
	)(isAuthObject)
}

// msgRedaction runs the text rules over the message of a record.
func msgRedaction() reader.Reader[Endomorphism[string], Endomorphism[Record]] {
	msgLens := MakeRecordMsgLens()

	return F.Flow2(
		lens.Modify[Record, Endomorphism[string]],
		reader.Read[Endomorphism[Record]](msgLens),
	)
}

// dataRedaction runs the text rules over every string in the data payload of
// a record, see [redactData].
func dataRedaction() reader.Reader[Endomorphism[string], result.Kleisli[Record, Record]] {
	dataLens := MakeRecordDataLens()

	return F.Flow3(
		redactData(),
		lens.ModifyF[Record, json.RawMessage, Result[json.RawMessage], Result[Record]](result.Map[json.RawMessage, Record]),
		reader.Read[result.Kleisli[Record, Record]](dataLens),
	)
}

// Redact removes sensitive values from a record: text runs over the message and
// over every string in the data payload, and Auth error payloads are replaced
// completely. It fails only if the payload is not valid JSON.
//
// The text rules are read by the message and the data stage, so both are
// composed with the reader applicative, after the Auth stage.
//
// Example:
//
//	redact := Redact()(F.Flow2(RedactText(), RedactTerms("jane-doe")))
//	records := iterresult.ChainEitherK(redact)(ReadLogFile()(path))
func Redact() reader.Reader[Endomorphism[string], result.Kleisli[Record, Record]] {
	return F.Pipe3(
		msgRedaction(),
		reader.Map[Endomorphism[string]](reader.Compose[Result[Record], Record, Record]),
		reader.Ap[result.Kleisli[Record, Record]](dataRedaction()),
		reader.Map[Endomorphism[string]](reader.Local[Result[Record]](redactAuthData())),
	)
}
