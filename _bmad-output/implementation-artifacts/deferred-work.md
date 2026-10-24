# Deferred work

## Deferred from: code review of E3 Steering (2026-10-23)

- **Steered messages are lost when the request after the poll fails** (`session/loop.go` `Run`).
  `Steer` drains the queue into the session, then `Next` fails (HTTP error, cancellation). `Run`
  returns only the error, so the session and the drained user messages are gone. The session
  itself was already lost on failure before E3; E4 ("return the partial session as a `Land`
  result") is the place to keep both.
