package session

import (
	"github.com/Carsten-Leue/fp-go-harness/tools"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/endomorphism"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/iooption"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/optics/iso"
	"github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	"github.com/IBM/fp-go/v2/tailrec"
)

// The session-changing tools (E6) at the [Session]: a [SessionCall] returns
// its text and an endomorphism of the session, see [tools.ToolResult]. A
// plain tool lifts into a [SessionTool] with [tools.LiftTool].
type (
	ToolResult      = tools.ToolResult[Session]
	SessionCall     = tools.StateCall[Session]
	SessionTool     = tools.StateTool[Session]
	SessionRegistry = tools.StateRegistry[Session]
	ToolCaller      = tools.StateToolCaller[Session]
	ToolDeps        = tools.StateToolDeps[Session]
)

type (
	Effect[C, A any]     = effect.Effect[C, A]
	Trampoline[B, L any] = tailrec.Trampoline[B, L]
	Thunk[A any]         = effect.Thunk[A]
	Endomorphism[A any]  = endomorphism.Endomorphism[A]
	Reader[R, A any]     = reader.Reader[R, A]
	Pair[L, R any]       = pair.Pair[L, R]
	Prism[S, A any]      = prism.Prism[S, A]
	Predicate[A any]     = predicate.Predicate[A]
	Iso[S, A any]        = iso.Iso[S, A]
	IOResult[A any]      = ioresult.IOResult[A]
	Result[A any]        = result.Result[A]
	IO[A any]            = io.IO[A]
	Option[A any]        = option.Option[A]
	IOOption[A any]      = iooption.IOOption[A]
)
