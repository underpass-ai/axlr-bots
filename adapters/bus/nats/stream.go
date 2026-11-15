package nats

import (
	"context"
	"errors"
	"slices"

	"github.com/nats-io/nats.go/jetstream"
)

// defineAttempts bounds how often defineStream reads the stream again
// after another connection created it between the read and the create.
const defineAttempts = 3

// defineStream makes the stream what want describes: it creates one that
// is missing, updates one that differs in what axlr-bots defines, and
// leaves alone one that already matches. Every connection defines its
// streams, so most find them as they want them; asking the broker to
// update one anyway rewrites its store's configuration, and nats-server
// does that racing a consumer created at the same moment.
func defineStream(ctx context.Context, js jetstream.JetStream, want jetstream.StreamConfig) error {
	for attempt := 1; ; attempt++ {
		stream, err := js.Stream(ctx, want.Name)
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			_, err = js.CreateStream(ctx, want)
			if errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) && attempt < defineAttempts {
				// Another connection created it, with another
				// configuration: read it again and compare.
				continue
			}
			return err
		}
		if err != nil {
			return err
		}
		if !streamDiffers(stream.CachedInfo().Config, want) {
			return nil
		}
		_, err = js.UpdateStream(ctx, want)
		return err
	}
}

// streamDiffers reports whether the stream the broker has differs from
// want in a field axlr-bots defines. The broker fills the others with
// its defaults, so they are not compared.
func streamDiffers(have, want jetstream.StreamConfig) bool {
	return !slices.Equal(have.Subjects, want.Subjects) ||
		have.Retention != want.Retention ||
		have.Storage != want.Storage ||
		have.MaxAge != want.MaxAge ||
		unlimited(have.MaxBytes) != unlimited(want.MaxBytes) ||
		have.Duplicates != want.Duplicates ||
		have.Discard != want.Discard
}

// unlimited reads a limit as the broker keeps it: zero, unset, is -1.
func unlimited(limit int64) int64 {
	if limit == 0 {
		return -1
	}
	return limit
}
