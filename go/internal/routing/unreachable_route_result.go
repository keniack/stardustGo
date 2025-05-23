package routing

import "github.com/keniack/stardustGo/pkg/types"

type UnreachableRouteResult struct{}

var UnreachableRouteResultInstance = &UnreachableRouteResult{}

func (r *UnreachableRouteResult) Reachable() bool {
	return false
}

func (r *UnreachableRouteResult) Latency() int {
	return 0
}

func (r *UnreachableRouteResult) WaitLatencyAsync() error {
	return nil
}

func (r *UnreachableRouteResult) AddCalculationDuration(ms int) types.IRouteResult {
	return r
}
