package githubhooks

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"
)

// RedeliverOptions says which deliveries Redeliver looks at.
type RedeliverOptions struct {
	// Repos are the repositories whose hooks are read.
	Repos []string
	// HookURL names the hook by its config.url; empty: the hooks whose
	// URL's path is Path.
	HookURL string
	// Since is how far back a failure is redelivered.
	Since time.Duration
	// Now is the clock's reading.
	Now time.Time
}

// Redelivery is one delivery Redeliver asked GitHub to attempt again.
type Redelivery struct {
	Repo        string `json:"repo"`
	Hook        int    `json:"hook"`
	Delivery    int    `json:"delivery"`
	GUID        string `json:"guid"`
	Event       string `json:"event,omitempty"`
	StatusCode  int    `json:"statusCode"`
	DeliveredAt string `json:"deliveredAt"`
}

// Redeliver asks GitHub to attempt again, once per GUID, the newest
// attempt of each event whose every attempt failed (a status that is
// not 2xx, 0 when nothing answered) and whose newest attempt is within
// Since. An event with any successful attempt was published: it is left.
// It goes on past a repository's error and returns them all, joined.
func Redeliver(ctx context.Context, api HookAPI, options RedeliverOptions) ([]Redelivery, error) {
	redelivered := []Redelivery{}
	var errs []error
	cutoff := options.Now.Add(-options.Since)
	for _, repo := range options.Repos {
		hooks, err := api.Hooks(ctx, repo)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", repo, err))
			continue
		}
		for _, hook := range hooks {
			if !hook.Active || !ours(hook.Config.URL, options.HookURL) {
				continue
			}
			deliveries, err := api.Deliveries(ctx, repo, hook.ID)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s hook %d: %w", repo, hook.ID, err))
				continue
			}
			for _, failed := range failedOnly(deliveries, cutoff) {
				if err := api.Redeliver(ctx, repo, hook.ID, failed.ID); err != nil {
					errs = append(errs, fmt.Errorf("%s hook %d delivery %d: %w", repo, hook.ID, failed.ID, err))
					continue
				}
				redelivered = append(redelivered, Redelivery{Repo: repo, Hook: hook.ID, Delivery: failed.ID, GUID: failed.GUID, Event: failed.Event, StatusCode: failed.StatusCode, DeliveredAt: failed.DeliveredAt})
			}
		}
	}
	return redelivered, errors.Join(errs...)
}

// ours reports whether a hook's URL is the adapter's: the one named, or
// any whose path is Path.
func ours(hookURL, want string) bool {
	if want != "" {
		return hookURL == want
	}
	parsed, err := url.Parse(hookURL)
	return err == nil && parsed.Path == Path
}

// failedOnly is the newest attempt of each GUID none of whose attempts
// succeeded, when it is not older than cutoff; oldest first.
func failedOnly(deliveries []HookDelivery, cutoff time.Time) []HookDelivery {
	newest := map[string]HookDelivery{}
	at := map[string]time.Time{}
	succeeded := map[string]bool{}
	for _, d := range deliveries {
		if d.GUID == "" {
			continue
		}
		if d.StatusCode >= 200 && d.StatusCode < 300 {
			succeeded[d.GUID] = true
			continue
		}
		when, err := time.Parse(time.RFC3339, d.DeliveredAt)
		if err != nil {
			continue
		}
		if prev, seen := newest[d.GUID]; !seen || when.After(at[d.GUID]) || (when.Equal(at[d.GUID]) && d.ID > prev.ID) {
			newest[d.GUID], at[d.GUID] = d, when
		}
	}
	var out []HookDelivery
	for guid, d := range newest {
		if succeeded[guid] || at[guid].Before(cutoff) {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := at[out[i].GUID], at[out[j].GUID]; !a.Equal(b) {
			return a.Before(b)
		}
		return out[i].ID < out[j].ID
	})
	return out
}
