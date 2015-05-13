package deployment

import "fmt"

type ErrSkipRollback struct {
	Err string
}

func (e ErrSkipRollback) Error() string {
	return e.Err
}

func IsSkipRollback(err error) bool {
	_, ok := err.(ErrSkipRollback)
	return ok
}

type UnknownStrategyError struct {
	Strategy string
}

func (e UnknownStrategyError) Error() string {
	return fmt.Sprintf("deployment: unknown strategy %q", e.Strategy)
}
