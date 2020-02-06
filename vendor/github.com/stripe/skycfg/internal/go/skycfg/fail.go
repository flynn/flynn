package skycfg

import (
	"fmt"

	"go.starlark.net/starlark"
)

var Fail = starlark.NewBuiltin("fail", failImpl)

func failImpl(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msg string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &msg); err != nil {
		return nil, err
	}
	callStack := t.CallStack()
	callStack.Pop()
	return nil, fmt.Errorf("[%s] %s\n%s", callStack.At(0).Pos, msg, callStack.String())
}
