package unixapi

import "errors"

func errorsAs(err error, target *RemoteError) bool { return errors.As(err, target) }
