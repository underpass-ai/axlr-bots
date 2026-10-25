package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// firstStart is what a node started with a token file does before it
// runs: what a sidecar does in a pod. Without credentials under its
// state it enrolls with the token in the file; with credentials it
// keeps them, unless the file holds a token other than the one they
// came from (a rotation). A token for another node id is refused before
// it is sent, so a wrong Secret never burns a token, and a rotation that
// fails keeps the certificate the node already has.
type firstStart struct {
	Control   string
	TokenFile string
	// CAFile pins the fleet CA in place of the one the token carries.
	CAFile   string
	StateDir string
	// NodeID is the id the workload expects (the pod's name); empty
	// accepts the token's.
	NodeID   string
	HostName string
}

// ensure makes one attempt and says what it did, for the log. It fails
// only when the node has no credentials to run with.
func (f firstStart) ensure(ctx context.Context) (string, error) {
	record, err := loadRecord(f.StateDir)
	if err != nil {
		return "", err
	}
	_, credsErr := pki.LoadCredentials(f.StateDir)
	enrolled := credsErr == nil
	keep := "keeping the certificate for node " + record.NodeID
	token, err := f.readToken()
	switch {
	case err == nil:
	case enrolled && errors.Is(err, os.ErrNotExist):
		return keep + " (no token file)", nil
	case enrolled:
		return keep + " (" + err.Error() + ")", nil
	default:
		return "", fmt.Errorf("not enrolled: %w", err)
	}
	if enrolled && (record.TokenID == "" || record.TokenID == token.ID) {
		return keep + " (token " + token.ID + " already used)", nil
	}
	done, err := f.enroll(ctx, token)
	if err != nil && enrolled {
		return keep + " (rotation: " + err.Error() + ")", nil
	}
	return done, err
}

// readToken reads and decodes the token file.
func (f firstStart) readToken() (nodeproto.EnrollToken, error) {
	raw, err := os.ReadFile(f.TokenFile)
	if errors.Is(err, os.ErrNotExist) {
		return nodeproto.EnrollToken{}, fmt.Errorf("no token at %s: %w", f.TokenFile, err)
	}
	if err != nil {
		return nodeproto.EnrollToken{}, err
	}
	token, err := nodeproto.DecodeEnrollToken(string(raw))
	if err != nil {
		return nodeproto.EnrollToken{}, fmt.Errorf("%s: %w", f.TokenFile, err)
	}
	return token, nil
}

// enroll checks the token names this node, then enrolls with it.
func (f firstStart) enroll(ctx context.Context, token nodeproto.EnrollToken) (string, error) {
	if f.NodeID != "" && token.NodeID != f.NodeID {
		return "", fmt.Errorf("the token at %s is for node %q and this node is %q: mint one with --node-id %s (this one was not used)", f.TokenFile, token.NodeID, f.NodeID, f.NodeID)
	}
	caPEM := []byte(token.CAPEM)
	if f.CAFile != "" {
		var err error
		if caPEM, err = os.ReadFile(f.CAFile); err != nil {
			return "", err
		}
	}
	result, err := enrollWith(ctx, f.Control, token, caPEM, f.StateDir, f.HostName)
	if err != nil {
		return "", fmt.Errorf("enroll with token %s: %w", token.ID, err)
	}
	return fmt.Sprintf("enrolled as node %s with token %s (serial %s, until %s)", result.NodeID, token.ID, result.Serial, result.NotAfter.UTC().Format("2006-01-02")), nil
}

// untilEnrolled retries ensure every pause until it succeeds or ctx
// ends: the control plane may not be reachable yet, and a wrong or
// spent token is fixed by updating the file (a Secret's update reaches
// the pod without a restart). A sidecar that waits keeps its pod
// Ready; one that exits takes the service out of its endpoints.
func (f firstStart) untilEnrolled(ctx context.Context, pause time.Duration, logf func(string, ...any)) bool {
	for {
		done, err := f.ensure(ctx)
		if err == nil {
			logf("node: %s", done)
			return true
		}
		logf("node: first start: %v (retrying in %s)", err, pause)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(pause):
		}
	}
}
