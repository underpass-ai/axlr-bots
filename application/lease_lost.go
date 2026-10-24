package application

import "errors"

// ErrLeaseLost is what a transport returns when the control plane refuses
// to renew a lease because the execution has ended (cancelled, or
// reconciled away) or belongs to another node: the node stops the run,
// since its report would be dropped.
var ErrLeaseLost = errors.New("lease lost: the execution has ended or belongs to another node")
